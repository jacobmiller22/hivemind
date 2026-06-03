package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jacobmiller22/hivemind/pkg/daemon"
)

// TestMultiClientBroadcasting verifies multi-client subscriptions and state tree event broadcasts using the native UDS server.
func TestMultiClientBroadcasting(t *testing.T) {
	socketPath := "/tmp/hivemind_test_main.sock"

	s := daemon.NewServer()
	s.SocketPath = socketPath
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

	resolvedSocketPath := daemon.ResolveSocketPath(socketPath)

	// Connect Client 1
	conn1, err := net.Dial("unix", resolvedSocketPath)
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
	conn2, err := net.Dial("unix", resolvedSocketPath)
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
	hookConn, err := net.Dial("unix", resolvedSocketPath)
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
