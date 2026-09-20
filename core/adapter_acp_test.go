package core

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestACPStringPermissionIDUsesOnceOption(t *testing.T) {
	var stdin bytes.Buffer
	s := &acpSession{
		stdin:       nopWriteCloser{&stdin},
		permission:  "allow",
		pending:     map[uint64]chan acpResponse{},
		pendingPerm: map[string]*pendingPerm{},
		events:      make(chan SessionEvent, 4),
		done:        make(chan struct{}),
	}
	params := json.RawMessage(`{"toolCall":{"title":"x"},"options":[{"optionId":"persist","name":"记住","kind":"allow_always"},{"optionId":"once","name":"仅一次","kind":"allow_once"}]}`)
	s.handleInboundRequest(json.RawMessage(`"permission-1"`), "session/request_permission", params)
	var frame struct {
		ID     string `json:"id"`
		Result struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdin.Bytes()), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.ID != "permission-1" || frame.Result.Outcome != "selected" || frame.Result.OptionID != "once" {
		t.Fatalf("permission response = %+v", frame)
	}
}

func TestACPProtocolEOFFailsPending(t *testing.T) {
	ch := make(chan acpResponse, 1)
	s := &acpSession{
		pending: map[uint64]chan acpResponse{1: ch},
		events:  make(chan SessionEvent, 4),
		done:    make(chan struct{}),
	}
	s.readLoop(strings.NewReader(""))
	response := <-ch
	if response.err == nil || !strings.Contains(response.err.Error(), "stdout") {
		t.Fatalf("pending response = %+v", response)
	}
}

type nopWriteCloser struct{ *bytes.Buffer }

func (nopWriteCloser) Close() error { return nil }
