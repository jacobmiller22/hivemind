package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jacobmiller22/hivemind/pkg/daemon"
	"github.com/jacobmiller22/hivemind/pkg/daemon/adapters"
)

// TestMultiClientBroadcasting verifies multi-client subscriptions and state tree event broadcasts.
func TestMultiClientBroadcasting(t *testing.T) {
	socketPath := "/tmp/hivemind_test_main.sock"

	s := daemon.NewServer()
	s.Adapters = []daemon.ToolAdapter{adapters.NewGenericUDSAdapter(socketPath)}
	s.ListPanesFunc = func() ([]string, error) {
		return []string{"%1"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start(ctx)
	}()

	// Wait for the socket to initialize
	time.Sleep(50 * time.Millisecond)

	// Connect Client 1
	conn1, err := net.Dial("unix", adapters.ResolveSocketPath(socketPath))
	if err != nil {
		t.Fatalf("failed to dial socket: %v", err)
	}
	defer conn1.Close()

	// Subscribe Client 1
	subMsg := `{"type":"subscribe"}` + "\n"
	_, err = conn1.Write([]byte(subMsg))
	if err != nil {
		t.Fatalf("client 1 write failed: %v", err)
	}

	// Read initial state for Client 1
	reader1 := bufio.NewReader(conn1)
	line1, err := reader1.ReadString('\n')
	if err != nil {
		t.Fatalf("client 1 read failed: %v", err)
	}
	var state1 daemon.StateTree
	if err := json.Unmarshal([]byte(line1), &state1); err != nil {
		t.Fatalf("client 1 initial state unmarshal failed: %v", err)
	}
	if len(state1.Sessions) != 0 {
		t.Errorf("expected 0 sessions initially, got %d", len(state1.Sessions))
	}

	// Connect Client 2
	conn2, err := net.Dial("unix", adapters.ResolveSocketPath(socketPath))
	if err != nil {
		t.Fatalf("failed to dial socket for client 2: %v", err)
	}
	defer conn2.Close()

	// Subscribe Client 2
	_, err = conn2.Write([]byte(subMsg))
	if err != nil {
		t.Fatalf("client 2 write failed: %v", err)
	}

	// Read initial state for Client 2
	reader2 := bufio.NewReader(conn2)
	line2, err := reader2.ReadString('\n')
	if err != nil {
		t.Fatalf("client 2 read failed: %v", err)
	}
	var state2 daemon.StateTree
	if err := json.Unmarshal([]byte(line2), &state2); err != nil {
		t.Fatalf("client 2 initial state unmarshal failed: %v", err)
	}

	// Connect a hook adapter and write a session_started event
	hookConn, err := net.Dial("unix", adapters.ResolveSocketPath(socketPath))
	if err != nil {
		t.Fatalf("failed to dial socket for hook: %v", err)
	}
	defer hookConn.Close()

	event := daemon.HivemindEvent{
		SessionID: "session_broadcast",
		EventType: "session_started",
		Context: daemon.EventContext{
			TmuxPaneId: "%1",
		},
		Payload: daemon.EventPayload{
			Status: daemon.StatusIdle,
		},
	}
	eventJSON, _ := json.Marshal(event)
	_, err = hookConn.Write(append(eventJSON, '\n'))
	if err != nil {
		t.Fatalf("failed to send hook event: %v", err)
	}
	hookConn.Close()

	// Verify both Client 1 and Client 2 receive the state update
	updateLine1, err := reader1.ReadString('\n')
	if err != nil {
		t.Fatalf("client 1 failed to read broadcast: %v", err)
	}
	var updateState1 daemon.StateTree
	_ = json.Unmarshal([]byte(updateLine1), &updateState1)
	sess1, exists := updateState1.Sessions["session_broadcast"]
	if !exists {
		t.Fatal("client 1 did not receive the update (no session_broadcast)")
	}

	updateLine2, err := reader2.ReadString('\n')
	if err != nil {
		t.Fatalf("client 2 failed to read broadcast: %v", err)
	}
	var updateState2 daemon.StateTree
	_ = json.Unmarshal([]byte(updateLine2), &updateState2)
	sess2, exists := updateState2.Sessions["session_broadcast"]
	if !exists {
		t.Fatal("client 2 did not receive the update (no session_broadcast)")
	}

	// Check updates parity
	if sess1.TmuxPaneID != sess2.TmuxPaneID {
		t.Error("broadcast updates are not identical")
	}

	cancel()
	_ = <-errChan
}

// TestParseTranscriptFileSubagents verifies that parseTranscriptFile correctly parses both raw and double-escaped subagents arguments,
// extracts the conversationId from INVOKE_SUBAGENT done step content, and correctly links/marks the subagent.
func TestParseTranscriptFileSubagents(t *testing.T) {
	tmpDir := t.TempDir()
	transcriptPath := filepath.Join(tmpDir, "transcript.jsonl")

	lines := []string{
		`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"Hello"}`,
		// Step 1: PLANNER_RESPONSE with double-escaped subagents string inside args
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"invoke_subagent","args":{"Subagents":"[{\"Role\":\"Code Analyst\",\"TypeName\":\"research\"}]"}}]}`,
		// Step 2: INVOKE_SUBAGENT with conversationId in content
		`{"step_index":2,"source":"MODEL","type":"INVOKE_SUBAGENT","status":"DONE","content":"Created the following subagents:\n{\n  \"conversationId\": \"9c036cf3-623e-4f36-9700-68577dfc9226\"\n}"}`,
	}

	f, err := os.Create(transcriptPath)
	if err != nil {
		t.Fatalf("failed to create transcript file: %v", err)
	}
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("failed to write line: %v", err)
		}
	}
	f.Close()

	adapter := adapters.NewAntigravityAdapter("", 1*time.Second)
	fss, err := adapter.ParseTranscriptFile(transcriptPath, "parent_session_1")
	if err != nil {
		t.Fatalf("failed to parse transcript file: %v", err)
	}

	if len(fss.Subagents) != 1 {
		t.Fatalf("expected 1 subagent, got %d", len(fss.Subagents))
	}

	sa, ok := fss.Subagents["9c036cf3-623e-4f36-9700-68577dfc9226"]
	if !ok {
		t.Fatalf("expected subagent to be linked with ID '9c036cf3-623e-4f36-9700-68577dfc9226', but it is missing. Subagents: %+v", fss.Subagents)
	}

	if sa.Role != "Code Analyst" {
		t.Errorf("expected role 'Code Analyst', got '%s'", sa.Role)
	}
	if sa.TypeName != "research" {
		t.Errorf("expected type 'research', got '%s'", sa.TypeName)
	}
	if sa.Status != daemon.SubagentCompleted {
		t.Errorf("expected status 'completed', got '%s'", sa.Status)
	}
	if sa.CompletedAt == nil {
		t.Error("expected CompletedAt to be set, but it was nil")
	}
}
