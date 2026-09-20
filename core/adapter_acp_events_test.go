package core

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestACPAssistantThinkingEvent(t *testing.T) {
	s := &acpSession{
		remoteID: "r1",
		pending:  map[uint64]chan acpResponse{},
		events:   make(chan SessionEvent, 4),
		done:     make(chan struct{}),
	}
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"让我想想"}}}`))
	select {
	case e := <-s.events:
		if e.Kind != EventAssistantThinking || e.Text != "让我想想" {
			t.Fatalf("event = %+v", e)
		}
	default:
		t.Fatal("missing thinking event")
	}
}

func TestACPToolCallAndResultEvents(t *testing.T) {
	s := &acpSession{
		remoteID: "r1",
		pending:  map[uint64]chan acpResponse{},
		events:   make(chan SessionEvent, 8),
		done:     make(chan struct{}),
	}
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Read","status":"running"}}`))
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"tool_call_update","toolCallId":"t1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"ok"}}]}}`))
	var kinds []EventKind
	for i := 0; i < 2; i++ {
		select {
		case e := <-s.events:
			kinds = append(kinds, e.Kind)
			if e.Kind == EventToolCall && e.Title != "Read" {
				t.Fatalf("tool call = %+v", e)
			}
			if e.Kind == EventToolResult && e.Text != "ok" {
				t.Fatalf("tool result = %+v", e)
			}
		default:
			t.Fatalf("missing event %d", i)
		}
	}
	if kinds[0] != EventToolCall || kinds[1] != EventToolResult {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestACPPermissionEmitsVisibleEvent(t *testing.T) {
	s := &acpSession{
		remoteID:   "r1",
		permission: "reject",
		pending:    map[uint64]chan acpResponse{},
		events:     make(chan SessionEvent, 4),
		done:       make(chan struct{}),
	}
	s.stdin = nopWriteCloser{&bytes.Buffer{}}
	s.handleInboundRequest(json.RawMessage(`"p1"`), "session/request_permission", json.RawMessage(`{"sessionId":"r1","toolCall":{"title":"rm -rf"},"options":[{"optionId":"o1","name":"拒绝","kind":"reject_once"}]}`))
	select {
	case e := <-s.events:
		if e.Kind != EventPermission || e.Title != "rm -rf" {
			t.Fatalf("permission event = %+v", e)
		}
	default:
		t.Fatal("missing permission event")
	}
}

func TestACPIgnoresUnknownUpdate(t *testing.T) {
	s := &acpSession{
		remoteID: "r1",
		pending:  map[uint64]chan acpResponse{},
		events:   make(chan SessionEvent, 4),
		done:     make(chan struct{}),
	}
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"mystery_future_kind","text":"x"}}`))
	select {
	case e := <-s.events:
		t.Fatalf("unknown update should be ignored, got %+v", e)
	default:
	}
}

func TestACPContentBlockText(t *testing.T) {
	s := &acpSession{
		remoteID: "r1",
		pending:  map[uint64]chan acpResponse{},
		events:   make(chan SessionEvent, 4),
		done:     make(chan struct{}),
	}
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}`))
	select {
	case e := <-s.events:
		if e.Kind != EventAssistantText || e.Text != "hi" {
			t.Fatalf("event = %+v", e)
		}
	default:
		t.Fatal("missing content block event")
	}
}

func TestACPToolContentDiff(t *testing.T) {
	s := &acpSession{
		remoteID: "r1",
		pending:  map[uint64]chan acpResponse{},
		events:   make(chan SessionEvent, 8),
		done:     make(chan struct{}),
	}
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Edit","kind":"edit","status":"in_progress","content":[{"type":"diff","path":"a.go","oldText":"x","newText":"y"}]}}`))
	select {
	case e := <-s.events:
		if e.Kind != EventToolCall || !strings.Contains(e.Title, "a.go") || !strings.Contains(e.Detail, "---") {
			t.Fatalf("tool diff = %+v", e)
		}
	default:
		t.Fatal("missing tool diff event")
	}
}

func TestACPPlanAndUsageAndTitle(t *testing.T) {
	s := &acpSession{
		remoteID: "r1",
		pending:  map[uint64]chan acpResponse{},
		events:   make(chan SessionEvent, 8),
		done:     make(chan struct{}),
	}
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"plan","entries":[{"content":"第一步","status":"in_progress","priority":"high"}]}}`))
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"usage_update","used":10,"size":100}}`))
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"session_info_update","title":"我的会话"}}`))
	for _, want := range []EventKind{EventPlan, EventUsage, EventTitle} {
		select {
		case e := <-s.events:
			if e.Kind != want {
				t.Fatalf("want %s got %+v", want, e)
			}
		default:
			t.Fatalf("missing %s", want)
		}
	}
	if s.SessionTitle() != "我的会话" {
		t.Fatalf("title = %q", s.SessionTitle())
	}
	if got := s.Usage(); got.Used != 10 || got.Size != 100 {
		t.Fatalf("usage = %+v", got)
	}
}

func TestACPModeAndCommands(t *testing.T) {
	s := &acpSession{
		remoteID: "r1",
		pending:  map[uint64]chan acpResponse{},
		events:   make(chan SessionEvent, 8),
		done:     make(chan struct{}),
	}
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"current_mode_update","currentModeId":"plan"}}`))
	s.handleNotification("session/update", json.RawMessage(`{"sessionId":"r1","update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"review","description":"看代码"}]}}`))
	var kinds []EventKind
	for i := 0; i < 2; i++ {
		select {
		case e := <-s.events:
			kinds = append(kinds, e.Kind)
		default:
			t.Fatalf("missing event %d", i)
		}
	}
	if kinds[0] != EventMode || kinds[1] != EventCommand {
		t.Fatalf("kinds = %v", kinds)
	}
	if s.CurrentMode() != "plan" || len(s.AvailableCommands()) != 1 {
		t.Fatalf("mode=%q cmds=%v", s.CurrentMode(), s.AvailableCommands())
	}
}

func TestACPAskPermissionSuspends(t *testing.T) {
	var stdin bytes.Buffer
	s := &acpSession{
		remoteID:    "r1",
		stdin:       nopWriteCloser{&stdin},
		permission:  "ask",
		pending:     map[uint64]chan acpResponse{},
		pendingPerm: map[string]*pendingPerm{},
		events:      make(chan SessionEvent, 4),
		done:        make(chan struct{}),
	}
	s.handleInboundRequest(json.RawMessage(`"p1"`), "session/request_permission", json.RawMessage(`{"sessionId":"r1","toolCall":{"title":"rm","kind":"execute"},"options":[{"optionId":"yes","name":"允许","kind":"allow_once"},{"optionId":"no","name":"拒绝","kind":"reject_once"}]}`))
	if stdin.Len() != 0 {
		t.Fatalf("ask should suspend, but wrote %q", stdin.String())
	}
	select {
	case e := <-s.events:
		if e.Kind != EventPermission || e.PermID == "" || len(e.Options) != 2 {
			t.Fatalf("permission event = %+v", e)
		}
	default:
		t.Fatal("missing ask permission event")
	}
	if err := s.DecidePermission(context.Background(), `"p1"`, "yes"); err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Result struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdin.Bytes()), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Result.Outcome != "selected" || frame.Result.OptionID != "yes" {
		t.Fatalf("decide = %+v", frame)
	}
}

func TestACPCancelRespondsPendingPermissions(t *testing.T) {
	var stdin bytes.Buffer
	s := &acpSession{
		remoteID:    "r1",
		stdin:       nopWriteCloser{&stdin},
		permission:  "ask",
		pending:     map[uint64]chan acpResponse{},
		pendingPerm: map[string]*pendingPerm{},
		events:      make(chan SessionEvent, 4),
		done:        make(chan struct{}),
	}
	s.handleInboundRequest(json.RawMessage(`"p9"`), "session/request_permission", json.RawMessage(`{"sessionId":"r1","options":[{"optionId":"no","kind":"reject_once"}]}`))
	if err := s.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdin.String(), "cancelled") {
		t.Fatalf("cancel should answer pending, got %q", stdin.String())
	}
}

func TestACPFSReadWrite(t *testing.T) {
	dir := t.TempDir()
	s := &acpSession{
		pending: map[uint64]chan acpResponse{},
		events:  make(chan SessionEvent, 4),
		done:    make(chan struct{}),
	}
	var stdin bytes.Buffer
	s.stdin = nopWriteCloser{&stdin}
	path := dir + "/a.txt"
	s.handleFSWrite(json.RawMessage(`1`), json.RawMessage(`{"path":`+quoteJSON(path)+`,"content":"hello"}`))
	var wframe struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdin.Bytes()), &wframe); err != nil || wframe.Error != nil {
		t.Fatalf("write = %+v err=%v", wframe, err)
	}
	stdin.Reset()
	s.handleFSRead(json.RawMessage(`2`), json.RawMessage(`{"path":`+quoteJSON(path)+`}`))
	var rframe struct {
		Result struct {
			Content string `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdin.Bytes()), &rframe); err != nil || rframe.Result.Content != "hello" {
		t.Fatalf("read = %+v err=%v", rframe, err)
	}
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
