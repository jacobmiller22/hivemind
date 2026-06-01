package daemon

import (
	"testing"
	"time"
)

// TestEventProcessing verifies that all event types transition the state tree correctly.
func TestEventProcessing(t *testing.T) {
	s := NewServer()
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
	s := NewServer()
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



