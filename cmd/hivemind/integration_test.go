package main_test

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jacobmiller22/hivemind/pkg/client"
)

func TestEndToEndIntegration(t *testing.T) {
	// Paths using an isolated temporary directory
	tmpDir := t.TempDir()
	testSocket := "/tmp/hivemind_integration.sock"
	testBinary := filepath.Join(tmpDir, "hivemind_integration_bin")
	testSessionsDir := filepath.Join(tmpDir, "sessions")
	testAntigravityDir := filepath.Join(tmpDir, "antigravity")

	_ = os.Remove(testSocket)
	defer os.Remove(testSocket)

	// 1. Compile the cmd/hivemind code to ensure we test the latest implementation
	t.Log("[*] Compiling hivemind binary for integration test...")
	buildCmd := exec.Command("go", "build", "-o", testBinary, ".")
	buildCmd.Dir = "" // run in the package's directory (cmd/hivemind)
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to compile binary: %v, output: %s", err, string(output))
	}
	t.Log("[+] Compilation successful")

	// 2. Spawn the daemon in the background using the integration test socket
	t.Logf("[*] Spawning background daemon on socket: %s", testSocket)
	cmdDaemon := exec.Command(testBinary, "-uds", testSocket, "-sessions-dir", testSessionsDir, "-antigravity-dir", testAntigravityDir, "daemon")
	cmdDaemon.Dir = "../../" // run relative to repo root
	
	// Create log file in the isolated temp directory
	logFile, err := os.Create(filepath.Join(tmpDir, "hivemind_integration_daemon.log"))
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	defer logFile.Close()
	cmdDaemon.Stdout = logFile
	cmdDaemon.Stderr = logFile

	if err := cmdDaemon.Start(); err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}
	defer func() {
		t.Log("[*] Killing daemon process...")
		_ = cmdDaemon.Process.Kill()
		_ = cmdDaemon.Wait()
	}()

	// Wait up to 2 seconds for the UDS socket file to be created
	t.Log("[*] Waiting for daemon socket to initialize...")
	socketCreated := false
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(testSocket); err == nil {
			socketCreated = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !socketCreated {
		logContent, _ := os.ReadFile(filepath.Join(tmpDir, "hivemind_integration_daemon.log"))
		t.Fatalf("UDS socket was not created at %s within timeout. Daemon log:\n%s", testSocket, string(logContent))
	}
	t.Log("[+] Daemon socket initialized successfully")

	// 3. Connect a TUI client subscriber to read event broadcasts
	t.Log("[*] Connecting subscriber client to socket...")
	conn, err := net.Dial("unix", testSocket)
	if err != nil {
		t.Fatalf("Failed to dial test socket: %v", err)
	}
	defer conn.Close()

	// Subscribe
	_, err = conn.Write([]byte(`{"type":"subscribe"}` + "\n"))
	if err != nil {
		t.Fatalf("Failed to write subscription message: %v", err)
	}

	// Channel to receive state tree broadcasts
	broadcastsChan := make(chan client.StateTree, 20)
	ctx, cancel := contextWithTimeout(t, 6*time.Second)
	defer cancel()

	// Spawn background reader routine
	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		reader := bufio.NewReader(conn)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				line, err := reader.ReadBytes('\n')
				if err != nil {
					return
				}
				var state client.StateTree
				if err := json.Unmarshal(line, &state); err == nil {
					broadcastsChan <- state
				}
			}
		}
	}()

	// 4. Run the python mock emitter script to stream agent telemetry events
	t.Log("[*] Executing Python mock emitter in client mode...")
	emitterCmd := exec.Command("python3", "src/hooks/mock_emitter.py", "--socket", testSocket, "--delay", "0.02")
	emitterCmd.Dir = "../../" // two directories up from cmd/hivemind
	
	if output, err := emitterCmd.CombinedOutput(); err != nil {
		t.Fatalf("Python mock emitter failed: %v, output: %s", err, string(output))
	}
	t.Log("[+] Mock emitter events successfully sent")

	// 5. Gather received states and perform in depth validation
	t.Log("[*] Validating received state tree broadcasts...")
	var finalState client.StateTree
	foundThinking := false
	foundSubagentRunning := false
	foundSubagentCompleted := false
	foundPermissionPrompt := false
	foundAwaitingInput := false

	assertionLoop:
	for {
		select {
		case state := <-broadcastsChan:
			finalState = state
			// Check sessions
			for _, session := range state.Sessions {
				// A. Check for parent session states
				if strings.HasPrefix(session.SessionID, "session_parent") {
					if session.Status == "thinking" {
						foundThinking = true
					}
					if session.Status == "awaiting-input" {
						foundAwaitingInput = true
					}

					// B. Check for child subagent lifecycles
					for _, sa := range session.Subagents {
						if sa.Role == "Code Optimizer" {
							if sa.Status == "running" {
								foundSubagentRunning = true
							}
							if sa.Status == "completed" {
								foundSubagentCompleted = true
							}
						}
					}
				}

				// C. Check for child subagent session status changes
				if strings.HasPrefix(session.SessionID, "session_child") {
					if session.Status == "awaiting-permission" {
						foundPermissionPrompt = true
					}
				}
			}
		case <-ctx.Done():
			break assertionLoop
		}
	}

	// 6. In-depth assertions proving the tools are fully working
	t.Log("[*] Asserting E2E integrations...")
	
	// A. Validate event sequencing transitions
	if !foundThinking {
		t.Error("FAIL: Integration test did not observe parent session status transitioning to 'thinking'")
	} else {
		t.Log("✔ Parent transitioned to 'thinking'")
	}

	if !foundSubagentRunning {
		t.Error("FAIL: Integration test did not observe subagent spawning with status 'running'")
	} else {
		t.Log("✔ Subagent successfully spawned and tracked as 'running'")
	}

	if !foundPermissionPrompt {
		t.Error("FAIL: Integration test did not observe subagent session hitting 'awaiting-permission' status")
	} else {
		t.Log("✔ Subagent session correctly broadcasted 'awaiting-permission' status")
	}

	if !foundSubagentCompleted {
		t.Error("FAIL: Integration test did not observe parent registering subagent transition to 'completed'")
	} else {
		t.Log("✔ Subagent completed lifecycle resolved back in parent view")
	}

	if !foundAwaitingInput {
		t.Error("FAIL: Integration test did not observe parent transitioning to 'awaiting-input' with question prompt")
	} else {
		t.Log("✔ Parent session correctly transitioned to 'awaiting-input'")
	}

	// B. Validate final tree structure sessions
	if len(finalState.Sessions) == 0 {
		t.Error("FAIL: Final state tree contains zero sessions")
	} else {
		for _, session := range finalState.Sessions {
			t.Logf("  ├─ Active Agent Pane: %s (Status: %s, Model: %s)", session.TmuxPaneID, session.Status, session.Model)
			for _, sa := range session.Subagents {
				t.Logf("  │  └─ Active Subagent: %s (Status: %s, Type: %s)", sa.Role, sa.Status, sa.TypeName)
			}
		}
	}

	cancel()
	_ = conn.Close() // Close connection to unblock background reader
	readWg.Wait()
}

// Helper context with timeout to simplify tests
type contextCancelFunc func()

type mockCtx struct {
	done chan struct{}
}

func (c *mockCtx) Done() <-chan struct{} {
	return c.done
}

func contextWithTimeout(t *testing.T, d time.Duration) (*mockCtx, contextCancelFunc) {
	done := make(chan struct{})
	c := &mockCtx{done: done}
	
	timer := time.AfterFunc(d, func() {
		close(done)
	})

	return c, func() {
		timer.Stop()
		select {
		case <-done:
		default:
			close(done)
		}
	}
}

func TestFileTailingIntegration(t *testing.T) {
	// Paths using an isolated temporary directory
	tmpDir := t.TempDir()
	testSocket := "/tmp/hivemind_file_integration.sock"
	testBinary := filepath.Join(tmpDir, "hivemind_file_integration_bin")
	testBrainDir := filepath.Join(tmpDir, "antigravity")
	testSessionsDir := filepath.Join(tmpDir, "sessions")

	_ = os.Remove(testSocket)
	defer os.Remove(testSocket)

	// 1. Compile the cmd/hivemind code
	t.Log("[*] Compiling hivemind binary for integration test...")
	buildCmd := exec.Command("go", "build", "-o", testBinary, ".")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to compile binary: %v, output: %s", err, string(output))
	}
	t.Log("[+] Compilation successful")

	// 2. Spawn the daemon with -enabled-tools=antigravity,uds, custom -antigravity-dir, and custom -sessions-dir
	t.Logf("[*] Spawning background daemon on socket: %s", testSocket)
	cmdDaemon := exec.Command(testBinary, "-uds", testSocket, "-enabled-tools", "antigravity,uds", "-antigravity-dir", testBrainDir, "-sessions-dir", testSessionsDir, "-file-poll", "10ms", "daemon")
	
	// Create log file in the isolated temp directory
	logFile, err := os.Create(filepath.Join(tmpDir, "hivemind_file_integration_daemon.log"))
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	defer logFile.Close()
	cmdDaemon.Stdout = logFile
	cmdDaemon.Stderr = logFile

	if err := cmdDaemon.Start(); err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}
	defer func() {
		t.Log("[*] Killing daemon process...")
		_ = cmdDaemon.Process.Kill()
		_ = cmdDaemon.Wait()
	}()

	// Wait up to 2 seconds for the UDS socket file to be created
	t.Log("[*] Waiting for daemon socket to initialize...")
	socketCreated := false
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(testSocket); err == nil {
			socketCreated = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !socketCreated {
		logContent, _ := os.ReadFile(filepath.Join(tmpDir, "hivemind_file_integration_daemon.log"))
		t.Fatalf("UDS socket was not created at %s within timeout. Daemon log:\n%s", testSocket, string(logContent))
	}
	t.Log("[+] Daemon socket initialized successfully")

	// 3. Connect a TUI client subscriber to read event broadcasts
	t.Log("[*] Connecting subscriber client to socket...")
	conn, err := net.Dial("unix", testSocket)
	if err != nil {
		t.Fatalf("Failed to dial test socket: %v", err)
	}
	defer conn.Close()

	// Subscribe
	_, err = conn.Write([]byte(`{"type":"subscribe"}` + "\n"))
	if err != nil {
		t.Fatalf("Failed to write subscription message: %v", err)
	}

	// Channel to receive state tree broadcasts
	broadcastsChan := make(chan client.StateTree, 50)
	ctx, cancel := contextWithTimeout(t, 6*time.Second)
	defer cancel()

	// Spawn background reader routine
	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		reader := bufio.NewReader(conn)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				line, err := reader.ReadBytes('\n')
				if err != nil {
					return
				}
				var state client.StateTree
				if err := json.Unmarshal(line, &state); err == nil {
					broadcastsChan <- state
				}
			}
		}
	}()

	// 4. Run the python mock emitter in transcript mode
	t.Log("[*] Executing Python mock file emitter in transcript mode...")
	emitterCmd := exec.Command("python3", "src/hooks/mock_file_emitter.py", "--sessions-dir", testBrainDir, "--mode", "transcript", "--delay", "0.02")
	emitterCmd.Dir = "../../" // two directories up from cmd/hivemind
	
	if output, err := emitterCmd.CombinedOutput(); err != nil {
		t.Fatalf("Python mock file emitter failed: %v, output: %s", err, string(output))
	}
	t.Log("[+] Mock emitter events successfully written to transcripts")

	// 5. Gather received states and perform in depth validation
	t.Log("[*] Validating received state tree broadcasts...")
	var finalState client.StateTree
	foundThinking := false
	foundToolRunning := false
	foundSubagentRunning := false
	foundSubagentCompleted := false
	foundPermissionPrompt := false
	foundAwaitingInput := false
	foundIdle := false

	assertionLoop:
	for {
		select {
		case state := <-broadcastsChan:
			finalState = state
			// Check sessions
			for _, session := range state.Sessions {
				// Check for parent session states (prefixed with file:)
				if strings.HasPrefix(session.SessionID, "session_parent") {
					if session.Status == "thinking" {
						foundThinking = true
					}
					if session.Status == "tool-running" {
						foundToolRunning = true
					}
					if session.Status == "awaiting-permission" {
						foundPermissionPrompt = true
					}
					if session.Status == "awaiting-input" {
						foundAwaitingInput = true
					}
					if session.Status == "idle" {
						foundIdle = true
					}

					// Check for subagents
					for _, sa := range session.Subagents {
						if sa.Role == "Code Optimizer" {
							if sa.Status == "running" {
								foundSubagentRunning = true
							}
							if sa.Status == "completed" {
								foundSubagentCompleted = true
							}
						}
					}
				}
			}
		case <-ctx.Done():
			break assertionLoop
		}
	}

	// 6. Assertions
	t.Log("[*] Asserting E2E transcript tailing integrations...")
	if !foundThinking {
		t.Error("FAIL: Did not observe parent session status transitioning to 'thinking'")
	} else {
		t.Log("✔ Parent transitioned to 'thinking'")
	}

	if !foundToolRunning {
		t.Error("FAIL: Did not observe parent session status transitioning to 'tool-running'")
	} else {
		t.Log("✔ Parent transitioned to 'tool-running'")
	}

	if !foundSubagentRunning {
		t.Error("FAIL: Did not observe subagent spawning with status 'running'")
	} else {
		t.Log("✔ Subagent successfully tracked as 'running'")
	}

	if !foundSubagentCompleted {
		t.Error("FAIL: Did not observe subagent transitioning to 'completed'")
	} else {
		t.Log("✔ Subagent successfully tracked as 'completed'")
	}

	if !foundPermissionPrompt {
		t.Error("FAIL: Did not observe parent session status transitioning to 'awaiting-permission'")
	} else {
		t.Log("✔ Parent transitioned to 'awaiting-permission'")
	}

	if !foundAwaitingInput {
		t.Error("FAIL: Did not observe parent session status transitioning to 'awaiting-input'")
	} else {
		t.Log("✔ Parent transitioned to 'awaiting-input'")
	}

	if !foundIdle {
		t.Error("FAIL: Did not observe parent session status returning to 'idle'")
	} else {
		t.Log("✔ Parent successfully returned to 'idle'")
	}

	// B. Validate final tree structure sessions
	if len(finalState.Sessions) > 0 {
		for _, session := range finalState.Sessions {
			t.Logf("✔ Verified active session: %s", session.SessionID)
		}
	} else {
		t.Log("✔ Final state tree pruned successfully (session folder removed)")
	}

	cancel()
	_ = conn.Close() // Close connection to unblock background reader
	readWg.Wait()
}

