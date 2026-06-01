package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEventProcessing verifies that all event types transition the state tree correctly.
func TestEventProcessing(t *testing.T) {
	s := NewServer("", "")
	s.SubagentCoolOff = 1 * time.Second
	s.SessionCoolOff = 1 * time.Second

	// 1. Send session_started event
	startedEvent := &HivemindEvent{
		SessionID: "session_1",
		EventType: "session_started",
		Timestamp: time.Now(),
		Context: EventContext{
			TmuxPaneId:  "%1",
			TmuxSession: "dev",
			TmuxWindow:  "0",
			Cwd:         "/path/to/project",
			GitBranch:   "main",
		},
		Payload: EventPayload{
			Status: StatusIdle,
			Model:  "Gemini 1.5 Pro",
		},
	}
	s.processEvent(startedEvent)

	state := s.GetState()
	sess, exists := state.Sessions["session_1"]
	if !exists {
		t.Fatal("session_1 was not created")
	}
	if sess.Status != StatusIdle {
		t.Errorf("expected status idle, got %s", sess.Status)
	}
	if sess.Model != "Gemini 1.5 Pro" {
		t.Errorf("expected model Gemini 1.5 Pro, got %s", sess.Model)
	}
	if sess.TmuxPaneID != "%1" {
		t.Errorf("expected TmuxPaneID %%1, got %s", sess.TmuxPaneID)
	}
	if sess.TmuxSession != "dev" {
		t.Errorf("expected TmuxSession dev, got %s", sess.TmuxSession)
	}

	// 2. Send status_changed event
	statusEvent := &HivemindEvent{
		SessionID: "session_1",
		EventType: "status_changed",
		Timestamp: time.Now(),
		Payload: EventPayload{
			Status: StatusThinking,
		},
	}
	s.processEvent(statusEvent)
	if sess.Status != StatusThinking {
		t.Errorf("expected status thinking, got %s", sess.Status)
	}

	// 3. Send subagent_spawned event
	spawnEvent := &HivemindEvent{
		SessionID: "session_1",
		EventType: "subagent_spawned",
		Timestamp: time.Now(),
		Payload: EventPayload{
			Subagent: &SubagentPayload{
				ID:       "sub_1",
				Role:     "researcher",
				TypeName: "codebase_researcher",
				Status:   "running",
			},
		},
	}
	s.processEvent(spawnEvent)
	sa, saExists := sess.Subagents["sub_1"]
	if !saExists {
		t.Fatal("subagent sub_1 was not spawned")
	}
	if sa.Role != "researcher" {
		t.Errorf("expected subagent role researcher, got %s", sa.Role)
	}
	if sa.Status != SubagentRunning {
		t.Errorf("expected subagent status running, got %s", sa.Status)
	}

	// 4. Send subagent_status_changed event
	saStatusEvent := &HivemindEvent{
		SessionID: "session_1",
		EventType: "subagent_status_changed",
		Timestamp: time.Now(),
		Payload: EventPayload{
			Subagent: &SubagentPayload{
				ID:     "sub_1",
				Status: "completed",
			},
		},
	}
	s.processEvent(saStatusEvent)
	if sa.Status != SubagentCompleted {
		t.Errorf("expected subagent status completed, got %s", sa.Status)
	}
	if sa.CompletedAt == nil {
		t.Fatal("completedAt was not set")
	}

	// 5. Send session_stopped event
	stopEvent := &HivemindEvent{
		SessionID: "session_1",
		EventType: "session_stopped",
		Timestamp: time.Now(),
	}
	s.processEvent(stopEvent)
	if sess.Status != StatusCompleted {
		t.Errorf("expected session status completed, got %s", sess.Status)
	}
	if !sess.PaneExited {
		t.Errorf("expected PaneExited to be true")
	}
}

// TestTmuxSyncAndPruning verifies that closed panes transition to completed and are eventually pruned.
func TestTmuxSyncAndPruning(t *testing.T) {
	s := NewServer("", "")
	s.SubagentCoolOff = 50 * time.Millisecond
	s.SessionCoolOff = 50 * time.Millisecond

	// Setup mock tmux active panes
	mockPanes := []string{"%1", "%2"}
	s.ListPanesFunc = func() ([]string, error) {
		return mockPanes, nil
	}

	// Create two sessions
	startedEvent1 := &HivemindEvent{
		SessionID: "session_1",
		EventType: "session_started",
		Timestamp: time.Now(),
		Context:   EventContext{TmuxPaneId: "%1"},
		Payload:   EventPayload{Status: StatusIdle},
	}
	startedEvent2 := &HivemindEvent{
		SessionID: "session_2",
		EventType: "session_started",
		Timestamp: time.Now(),
		Context:   EventContext{TmuxPaneId: "%2"},
		Payload:   EventPayload{Status: StatusIdle},
	}
	s.processEvent(startedEvent1)
	s.processEvent(startedEvent2)

	// Spawn a subagent in session_1
	spawnEvent := &HivemindEvent{
		SessionID: "session_1",
		EventType: "subagent_spawned",
		Timestamp: time.Now(),
		Payload: EventPayload{
			Subagent: &SubagentPayload{
				ID:     "sub_1",
				Status: "completed", // Spawned but already completed to test pruning
			},
		},
	}
	s.processEvent(spawnEvent)

	// Verify both sessions and subagent exist
	state := s.GetState()
	if len(state.Sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(state.Sessions))
	}
	if len(state.Sessions["session_1"].Subagents) != 1 {
		t.Errorf("expected 1 subagent, got %d", len(state.Sessions["session_1"].Subagents))
	}

	// 1. Run sync when panes are still active: no changes
	s.SyncTmuxAndPrune()
	state = s.GetState()
	if len(state.Sessions) != 2 {
		t.Errorf("expected 2 sessions after sync, got %d", len(state.Sessions))
	}

	// 2. Remove %2 from active panes: session_2 should mark as exited/completed
	mockPanes = []string{"%1"}
	s.SyncTmuxAndPrune()
	state = s.GetState()
	sess2 := state.Sessions["session_2"]
	if sess2 == nil {
		t.Fatal("session_2 should not be pruned immediately")
	}
	if sess2.Status != StatusCompleted {
		t.Errorf("expected status completed, got %s", sess2.Status)
	}
	if !sess2.PaneExited {
		t.Error("expected PaneExited to be true")
	}

	// 3. Wait for cool-off, then run sync again: session_2 and sub_1 should be pruned
	time.Sleep(80 * time.Millisecond)
	s.SyncTmuxAndPrune()
	state = s.GetState()

	if _, exists := state.Sessions["session_2"]; exists {
		t.Error("session_2 should have been pruned after cool-off")
	}
	sess1 := state.Sessions["session_1"]
	if sess1 == nil {
		t.Fatal("session_1 should still exist")
	}
	if _, exists := sess1.Subagents["sub_1"]; exists {
		t.Error("subagent sub_1 should have been pruned after cool-off")
	}
}

// TestMultiClientBroadcasting verifies multi-client subscriptions and state tree event broadcasts.
func TestMultiClientBroadcasting(t *testing.T) {
	socketPath := "/tmp/hivemind_test_main.sock"

	s := NewServer(socketPath, "")
	s.Adapters = []ToolAdapter{&GenericUDSAdapter{}}
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
	conn1, err := net.Dial("unix", ResolveSocketPath(socketPath))
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
	var state1 StateTree
	if err := json.Unmarshal([]byte(line1), &state1); err != nil {
		t.Fatalf("client 1 initial state unmarshal failed: %v", err)
	}
	if len(state1.Sessions) != 0 {
		t.Errorf("expected 0 sessions initially, got %d", len(state1.Sessions))
	}

	// Connect Client 2
	conn2, err := net.Dial("unix", ResolveSocketPath(socketPath))
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
	var state2 StateTree
	if err := json.Unmarshal([]byte(line2), &state2); err != nil {
		t.Fatalf("client 2 initial state unmarshal failed: %v", err)
	}

	// Connect a hook adapter and write a session_started event
	hookConn, err := net.Dial("unix", ResolveSocketPath(socketPath))
	if err != nil {
		t.Fatalf("failed to dial socket for hook: %v", err)
	}
	defer hookConn.Close()

	event := HivemindEvent{
		SessionID: "session_broadcast",
		EventType: "session_started",
		Context: EventContext{
			TmuxPaneId: "%1",
		},
		Payload: EventPayload{
			Status: StatusIdle,
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
	var updateState1 StateTree
	_ = json.Unmarshal([]byte(updateLine1), &updateState1)
	sess1, exists := updateState1.Sessions["session_broadcast"]
	if !exists {
		t.Fatal("client 1 did not receive the update (no session_broadcast)")
	}

	updateLine2, err := reader2.ReadString('\n')
	if err != nil {
		t.Fatalf("client 2 failed to read broadcast: %v", err)
	}
	var updateState2 StateTree
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

	s := NewServer("", "")
	fss, err := s.parseTranscriptFile(transcriptPath, "parent_session_1")
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
	if sa.Status != SubagentCompleted {
		t.Errorf("expected status 'completed', got '%s'", sa.Status)
	}
	if sa.CompletedAt == nil {
		t.Error("expected CompletedAt to be set, but it was nil")
	}
}

