package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const DefaultSessionsFile = "sessions.jsonl"

// SessionRecord 是可查询的本地 transcript；它不是远端 session resume token。
// RemoteID/Modes/Usage 是 live 能力快照，便于 TUI 展示与 resume 入口判断。
type SessionRecord struct {
	Schema    int           `json:"schema"`
	ID        SessionID     `json:"id"`
	Agent     string        `json:"agent"`
	Adapter   string        `json:"adapter"`
	Workdir   string        `json:"workdir"`
	Title     string        `json:"title,omitempty"`
	RemoteID  string        `json:"remoteId,omitempty"`
	Mode      string        `json:"mode,omitempty"`
	Usage     SessionUsage  `json:"usage,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Status    SessionStatus `json:"status"`
	Events    []StoredEvent `json:"events"`
}

// StoredEvent 保存安全的 canonical event；不保存 adapter env 或 provider 原始帧。
// Title/Name/Detail/ToolID 随 tool/thinking 事件持久化，老行缺失按零值解码。
type StoredEvent struct {
	Seq    uint64        `json:"seq"`
	Kind   EventKind     `json:"kind"`
	Text   string        `json:"text,omitempty"`
	Error  string        `json:"error,omitempty"`
	Status SessionStatus `json:"status,omitempty"`
	Title  string        `json:"title,omitempty"`
	Name   string        `json:"name,omitempty"`
	Detail string        `json:"detail,omitempty"`
	ToolID string        `json:"toolId,omitempty"`
	At     time.Time     `json:"at"`
}

type sessionLogLine struct {
	Schema int            `json:"schema"`
	Record *SessionRecord `json:"record,omitempty"`
	ID     SessionID      `json:"id,omitempty"`
	Event  *StoredEvent   `json:"event,omitempty"`
}

// SessionPatch 是 transcript 元数据的局部更新（标题/远端ID/模式/用量）。
type SessionPatch struct {
	Title    *string
	RemoteID *string
	Mode     *string
	Usage    *SessionUsage
}

// SessionStore 管理本地 transcript。每个操作都可在没有 live backend 时查询。
type SessionStore interface {
	Create(SessionRecord) error
	Append(SessionID, SessionEvent) error
	Update(SessionID, SessionPatch) error
	List() ([]SessionRecord, error)
	Get(SessionID) (*SessionRecord, error)
	// Delete 按本地 ID 删除一条 transcript（不碰远端）。
	Delete(SessionID) error
}

type FileSessionStore struct {
	path string
	mu   *sync.Mutex
	// mem 是进程内索引：Create 后常驻内存，Append 只追加内存 + 写一行，
	// 不再每次全量读文件。跨进程写入时按 mtime 失效重载。
	mem     map[SessionID]*SessionRecord
	memSize int64
	memTime time.Time
	memInit bool
}

var sessionFileLocks sync.Map // canonical path → *sync.Mutex，协调同一进程内的多个 Store 实例。

func NewFileSessionStore(base string) *FileSessionStore {
	path := filepath.Join(base, DefaultSessionsFile)
	value, _ := sessionFileLocks.LoadOrStore(filepath.Clean(path), &sync.Mutex{})
	return &FileSessionStore{path: path, mu: value.(*sync.Mutex)}
}

// ensureLocked 保证内存索引有效：未初始化或文件被外部改过则重载。
func (s *FileSessionStore) ensureLocked() error {
	st, err := os.Stat(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var size int64
	var mtime time.Time
	if err == nil {
		size, mtime = st.Size(), st.ModTime()
	}
	if s.memInit && size == s.memSize && mtime.Equal(s.memTime) {
		return nil
	}
	all, err := s.loadLocked()
	if err != nil {
		return err
	}
	s.mem = all
	s.memSize, s.memTime, s.memInit = size, mtime, true
	return nil
}

func (s *FileSessionStore) markDirtyLocked() {
	st, err := os.Stat(s.path)
	if err != nil {
		s.memSize, s.memTime = -1, time.Time{}
		return
	}
	s.memSize, s.memTime = st.Size(), st.ModTime()
}

func (s *FileSessionStore) Create(record SessionRecord) error {
	if s == nil || s.mu == nil {
		return fmt.Errorf("session store 未初始化")
	}
	if record.ID == "" || record.Agent == "" || record.Adapter == "" {
		return fmt.Errorf("session record 缺少 id、agent 或 adapter")
	}
	if record.Schema == 0 {
		record.Schema = 1
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	if record.Status == "" {
		record.Status = SessionReady
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	if s.mem[record.ID] != nil {
		return fmt.Errorf("session record 已存在：%s", record.ID)
	}
	if err := s.appendLineLocked(sessionLogLine{Schema: 1, Record: &record}); err != nil {
		return err
	}
	cp := record
	cp.Events = append([]StoredEvent(nil), record.Events...)
	s.mem[record.ID] = &cp
	s.markDirtyLocked()
	return nil
}

func (s *FileSessionStore) Append(id SessionID, event SessionEvent) error {
	if s == nil || s.mu == nil || id == "" {
		return fmt.Errorf("session append 缺少 store 或 id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	record := s.mem[id]
	if record == nil {
		return fmt.Errorf("会话不存在：%s", id)
	}
	stored := StoredEvent{Seq: uint64(len(record.Events) + 1), Kind: event.Kind, Text: event.Text, Error: event.Error, Status: event.Status, Title: event.Title, Name: event.Name, Detail: event.Detail, ToolID: event.ToolID, At: event.At}
	if stored.At.IsZero() {
		stored.At = time.Now().UTC()
	}
	if err := s.appendLineLocked(sessionLogLine{Schema: 1, ID: id, Event: &stored}); err != nil {
		return err
	}
	record.Events = append(record.Events, stored)
	record.UpdatedAt = stored.At
	if stored.Status != "" {
		record.Status = stored.Status
	}
	if event.Kind == EventTitle && event.Text != "" {
		record.Title = event.Text
		if err := s.appendLineLocked(sessionLogLine{Schema: 1, Record: record}); err != nil {
			return err
		}
	}
	s.markDirtyLocked()
	return nil
}

func (s *FileSessionStore) Update(id SessionID, patch SessionPatch) error {
	if s == nil || s.mu == nil || id == "" {
		return fmt.Errorf("session update 缺少 store 或 id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	record := s.mem[id]
	if record == nil {
		return fmt.Errorf("会话不存在：%s", id)
	}
	record.UpdatedAt = time.Now().UTC()
	if patch.Title != nil {
		record.Title = *patch.Title
	}
	if patch.RemoteID != nil {
		record.RemoteID = *patch.RemoteID
	}
	if patch.Mode != nil {
		record.Mode = *patch.Mode
	}
	if patch.Usage != nil {
		record.Usage = *patch.Usage
	}
	if err := s.appendLineLocked(sessionLogLine{Schema: 1, Record: record}); err != nil {
		return err
	}
	s.markDirtyLocked()
	return nil
}

func (s *FileSessionStore) Delete(id SessionID) error {
	if s == nil || s.mu == nil || id == "" {
		return fmt.Errorf("session delete 缺少 store 或 id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	if s.mem[id] == nil {
		return fmt.Errorf("会话不存在：%s", id)
	}
	delete(s.mem, id)
	if err := s.rewriteLocked(s.mem); err != nil {
		return err
	}
	s.markDirtyLocked()
	return nil
}

func (s *FileSessionStore) rewriteLocked(all map[SessionID]*SessionRecord) error {
	ids := make([]SessionID, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var buf bytes.Buffer
	for _, id := range ids {
		record := *all[id]
		record.Events = append([]StoredEvent(nil), all[id].Events...)
		b, err := json.Marshal(sessionLogLine{Schema: 1, Record: &record})
		if err != nil {
			return err
		}
		buf.Write(append(b, '\n'))
		for _, e := range all[id].Events {
			ev := e
			b, err := json.Marshal(sessionLogLine{Schema: 1, ID: id, Event: &ev})
			if err != nil {
				return err
			}
			buf.Write(append(b, '\n'))
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *FileSessionStore) List() ([]SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(); err != nil {
		return nil, err
	}
	out := make([]SessionRecord, 0, len(s.mem))
	for _, record := range s.mem {
		out = append(out, *record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *FileSessionStore) Get(id SessionID) (*SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(); err != nil {
		return nil, err
	}
	record := s.mem[id]
	if record == nil {
		return nil, nil
	}
	copy := *record
	copy.Events = append([]StoredEvent(nil), record.Events...)
	return &copy, nil
}

func (s *FileSessionStore) appendLine(line sessionLogLine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendLineLocked(line)
}

func (s *FileSessionStore) appendLineLocked(line sessionLogLine) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(line)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func (s *FileSessionStore) load() (map[SessionID]*SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *FileSessionStore) loadLocked() (map[SessionID]*SessionRecord, error) {
	content, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[SessionID]*SessionRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(content, []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) != 0 {
		// Append + Sync guarantees completed records end in newline. A nonempty trailing fragment
		// is a crash-tail and must not hide the valid prefix.
		lines = lines[:len(lines)-1]
	}

	out := map[SessionID]*SessionRecord{}
	for index, raw := range lines {
		if len(raw) == 0 {
			continue
		}
		lineNo := index + 1
		if len(raw) > acpMaxLineBytes {
			return nil, fmt.Errorf("sessions.jsonl 第 %d 行超过大小限制", lineNo)
		}
		var line sessionLogLine
		if err := json.Unmarshal(raw, &line); err != nil {
			return nil, fmt.Errorf("sessions.jsonl 第 %d 行损坏：%w", lineNo, err)
		}
		if line.Record != nil {
			record := *line.Record
			record.Events = append([]StoredEvent(nil), line.Record.Events...)
			out[record.ID] = &record
			continue
		}
		if line.Event != nil {
			record := out[line.ID]
			if record == nil {
				return nil, fmt.Errorf("sessions.jsonl 第 %d 行引用未知会话 %s", lineNo, line.ID)
			}
			record.Events = append(record.Events, *line.Event)
			record.UpdatedAt = line.Event.At
			if line.Event.Status != "" {
				record.Status = line.Event.Status
			}
		}
	}
	return out, nil
}
