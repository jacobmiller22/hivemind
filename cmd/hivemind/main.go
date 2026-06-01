package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jacobmiller22/hivemind"
	"github.com/jacobmiller22/hivemind/pkg/client"
	"github.com/jacobmiller22/hivemind/pkg/daemon"
	"github.com/jacobmiller22/hivemind/pkg/daemon/adapters"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	demoFlag := flag.Bool("demo", false, "Run in offline interactive demo mode with mock data")
	udsFlag := flag.String("uds", "", "Custom path to the Unix Domain Socket")
	enabledToolsFlag := flag.String("enabled-tools", "all", "Comma-separated list of tool adapters to enable (all, uds, mock-file, antigravity)")
	antigravityDirFlag := flag.String("antigravity-dir", "", "Custom path to search for Antigravity transcript files")
	sessionsDirFlag := flag.String("sessions-dir", "", "Custom path to look for mock sessions JSON files")
	filePollFlag := flag.String("file-poll", "1s", "Polling interval for file adapters (e.g. 1s, 500ms)")
	restartFlag := flag.Bool("restart", false, "Restart the background daemon if it is already running")
	flag.Parse()

	subcommand := ""
	if flag.NArg() > 0 {
		subcommand = flag.Arg(0)
	}

	switch subcommand {
	case "daemon":
		runDaemon(*udsFlag, *enabledToolsFlag, *antigravityDirFlag, *sessionsDirFlag, *filePollFlag)
	case "install-hooks":
		runInstallHooks()
	case "mock-file", "run-mock-file-emitter":
		runMockFileEmitter()
	case "hook":
		runHook(*udsFlag)
	case "help":
		printHelp()
	case "":
		runClient(*udsFlag, *demoFlag, *restartFlag)
	default:
		if subcommand == "-h" || subcommand == "--help" || subcommand == "help" {
			printHelp()
		} else {
			fmt.Printf("Unknown command: %s\n", subcommand)
			printHelp()
			os.Exit(1)
		}
	}
}

func runDaemon(customUdsPath, enabledTools, customAntigravityDir, customSessionsDir, filePoll string) {
	socketPath := customUdsPath
	if socketPath == "" {
		socketPath = client.ResolvePath("~/.config/hivemind/hivemind.sock")
	}
	fallbackPath := "/tmp/hivemind.sock"

	sessionsDir := customSessionsDir
	if sessionsDir == "" {
		sessionsDir = client.ResolvePath("~/.config/hivemind/sessions")
	} else {
		sessionsDir = client.ResolvePath(sessionsDir)
	}

	antigravityDir := customAntigravityDir
	if antigravityDir == "" {
		antigravityDir = client.ResolvePath("~/.gemini/antigravity-cli/brain")
	} else {
		antigravityDir = client.ResolvePath(antigravityDir)
	}

	s := daemon.NewServer()

	pollInterval := 1 * time.Second
	if pollDur, err := time.ParseDuration(filePoll); err == nil {
		pollInterval = pollDur
	}

	// Register adapters according to enabledTools config
	enabled := strings.ToLower(enabledTools)
	tools := strings.Split(enabled, ",")
	allMode := enabled == "all" || enabled == ""

	// Ensure stable deduplicated check
	addedTools := make(map[string]bool)
	for _, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool == "uds" || allMode {
			if !addedTools["uds"] {
				s.Adapters = append(s.Adapters, adapters.NewGenericUDSAdapter(socketPath))
				addedTools["uds"] = true
			}
		}
		if tool == "mock-file" || allMode {
			if !addedTools["mock-file"] {
				s.Adapters = append(s.Adapters, adapters.NewMockFileAdapter(sessionsDir, pollInterval))
				addedTools["mock-file"] = true
			}
		}
		if tool == "antigravity" || allMode {
			if !addedTools["antigravity"] {
				s.Adapters = append(s.Adapters, adapters.NewAntigravityAdapter(antigravityDir, pollInterval))
				addedTools["antigravity"] = true
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	fmt.Printf("Starting hivemind daemon...\n")
	fmt.Printf("Primary socket:  %s\n", socketPath)
	fmt.Printf("Fallback socket: %s\n", fallbackPath)
	fmt.Printf("Sessions dir:    %s\n", sessionsDir)
	fmt.Printf("Antigravity dir: %s\n", antigravityDir)

	var activeToolNames []string
	for _, a := range s.Adapters {
		activeToolNames = append(activeToolNames, a.Name())
	}
	fmt.Printf("Active tools:    %s\n", strings.Join(activeToolNames, ", "))

	pidFile := client.ResolvePath("~/.config/hivemind/daemon.pid")
	_ = os.MkdirAll(filepath.Dir(pidFile), 0755)
	_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
	defer os.Remove(pidFile)

	if err := s.Start(ctx); err != nil {
		fmt.Printf("Daemon error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Daemon stopped cleanly.\n")
}

func runInstallHooks() {
	fmt.Printf("Installing hivemind telemetry hooks...\n")

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error: Unable to find home directory: %v\n", err)
		os.Exit(1)
	}

	// 1. Copy the embedded python hooks to the plugin folder
	pluginDir := filepath.Join(home, ".gemini/config/plugins/hivemind_hooks")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		fmt.Printf("Error: Unable to create plugin directory %s: %v\n", pluginDir, err)
		os.Exit(1)
	}

	hookDest := filepath.Join(pluginDir, "hivemind_hooks.py")
	if err := os.WriteFile(hookDest, hivemind.HivemindHooksPy, 0644); err != nil {
		fmt.Printf("Error: Failed to write hook script to %s: %v\n", hookDest, err)
		os.Exit(1)
	}
	fmt.Printf("✔ Copied hivemind_hooks.py to %s\n", hookDest)

	// 2. Configure active shell profile PYTHONPATH
	exportCmd := fmt.Sprintf("\n# Added by hivemind installer\nexport PYTHONPATH=\"%s:$PYTHONPATH\"\n", pluginDir)

	profiles := []string{".zshrc", ".bashrc", ".bash_profile"}
	updatedCount := 0

	for _, profile := range profiles {
		profilePath := filepath.Join(home, profile)
		if _, err := os.Stat(profilePath); err == nil {
			// Read the file to see if it already contains the plugin path
			contentBytes, err := os.ReadFile(profilePath)
			if err != nil {
				continue
			}

			content := string(contentBytes)
			if strings.Contains(content, "plugins/hivemind_hooks") {
				fmt.Printf("✔ %s already configured with PYTHONPATH\n", profile)
				continue
			}

			// Append the export statement
			f, err := os.OpenFile(profilePath, os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				fmt.Printf("⚠ Failed to open shell profile %s for writing: %v\n", profile, err)
				continue
			}
			_, _ = f.WriteString(exportCmd)
			_ = f.Close()
			fmt.Printf("✔ Appended PYTHONPATH configuration to %s\n", profile)
			updatedCount++
		}
	}

	fmt.Printf("\nSetup completed successfully!\n")
	if updatedCount > 0 {
		fmt.Printf("👉 Please restart your terminal or run: source ~/%s (e.g. source ~/.zshrc) to apply environmental changes.\n", profiles[0])
	}
}

func runClient(udsPath string, runDemo bool, restartDaemon bool) {
	if udsPath == "" {
		udsPath = client.ResolvePath("~/.config/hivemind/hivemind.sock")
	}

	if restartDaemon {
		killExistingDaemon()
	}

	runDemoMode := runDemo
	if !runDemoMode {
		// Try to connect to UDS to see if daemon is alive
		conn, err := net.DialTimeout("unix", udsPath, 100*time.Millisecond)
		if err != nil {
			// Fallback check
			fallbackPath := "/tmp/hivemind.sock"
			connFallback, errFallback := net.DialTimeout("unix", fallbackPath, 100*time.Millisecond)
			if errFallback != nil {
				// Daemon is not running! Auto-spawn in background
				fmt.Printf("Daemon not running. Auto-spawning background daemon...\n")
				
				// Create config log directory
				logDir := client.ResolvePath("~/.config/hivemind")
				_ = os.MkdirAll(logDir, 0755)
				logFile, openErr := os.OpenFile(filepath.Join(logDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

				args := []string{"-uds", udsPath, "daemon"}
				cmd := exec.Command(os.Args[0], args...)
				if openErr == nil {
					cmd.Stdout = logFile
					cmd.Stderr = logFile
				}
				
				// Start detached daemon
				if errStart := cmd.Start(); errStart != nil {
					fmt.Printf("Warning: Failed to auto-spawn daemon: %v\n", errStart)
					// Fallback to offline demo mode so the user still gets a working dashboard!
					runDemoMode = true
				} else {
					// Poll the UNIX domain socket 'udsPath' until connection is established or timeout
					start := time.Now()
					timeout := 2000 * time.Millisecond
					connected := false
					for time.Since(start) < timeout {
						conn, err := net.DialTimeout("unix", udsPath, 50*time.Millisecond)
						if err == nil {
							conn.Close()
							connected = true
							break
						}
						time.Sleep(30 * time.Millisecond)
					}
					if !connected {
						fmt.Printf("Warning: Daemon auto-spawned, but UNIX domain socket did not become ready after %v. Degrading to demo mode.\n", timeout)
						runDemoMode = true
					}
				}
			} else {
				connFallback.Close()
				udsPath = fallbackPath
			}
		} else {
			conn.Close()
		}
	}

	m := client.NewModel(udsPath, runDemoMode)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running Bubble Tea program: %v\n", err)
		os.Exit(1)
	}
}

func runMockFileEmitter() {
	fmt.Printf("Extracting and running mock file emitter...\n")

	tempDir, err := os.MkdirTemp("", "hivemind_mock_*")
	if err != nil {
		fmt.Printf("Error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)

	scriptPath := filepath.Join(tempDir, "mock_file_emitter.py")
	if err := os.WriteFile(scriptPath, hivemind.MockFileEmitterPy, 0755); err != nil {
		fmt.Printf("Error writing mock file emitter: %v\n", err)
		os.Exit(1)
	}

	args := []string{scriptPath}
	// Append remaining command line arguments passed to hivemind subcommand
	if flag.NArg() > 1 {
		args = append(args, flag.Args()[1:]...)
	}

	cmd := exec.Command("python3", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		os.Exit(1)
	}
}

type AntigravityToolCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type AntigravityInput struct {
	ConversationID        string               `json:"conversationId"`
	WorkspacePaths        []string             `json:"workspacePaths"`
	TranscriptPath        string               `json:"transcriptPath"`
	ArtifactDirectoryPath string               `json:"artifactDirectoryPath"`
	ToolCall              *AntigravityToolCall `json:"toolCall,omitempty"`
	StepIdx               int                  `json:"stepIdx,omitempty"`
	Error                 string               `json:"error,omitempty"`
	InvocationNum         int                  `json:"invocationNum,omitempty"`
	InitialNumSteps       int                  `json:"initialNumSteps,omitempty"`
	ExecutionNum          int                  `json:"executionNum,omitempty"`
	TerminationReason     string               `json:"terminationReason,omitempty"`
	FullyIdle             bool                 `json:"fullyIdle,omitempty"`
}

func runHook(customUdsPath string) {
	// 1. Get event name from arguments
	event := ""
	if flag.NArg() > 1 {
		event = flag.Arg(1)
	}

	// 2. Read stdin
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		respondToAntigravity(event)
		return
	}

	var input AntigravityInput
	if len(stdinData) > 0 {
		if err := json.Unmarshal(stdinData, &input); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing JSON input: %v\n", err)
			respondToAntigravity(event)
			return
		}
	}

	// 3. Process event and send to UDS
	if input.ConversationID != "" {
		// Resolve Cwd
		cwd := ""
		if len(input.WorkspacePaths) > 0 {
			cwd = input.WorkspacePaths[0]
		}
		if cwd == "" {
			if w, err := os.Getwd(); err == nil {
				cwd = w
			}
		}

		// Resolve Git Branch
		gitBranch := ""
		gitCmd := exec.Command("git", "branch", "--show-current")
		gitCmd.Dir = cwd
		if out, err := gitCmd.Output(); err == nil {
			gitBranch = strings.TrimSpace(string(out))
		}

		// Resolve Tmux Pane Coordinates
		tmuxPaneID := os.Getenv("TMUX_PANE")
		var tmuxSession, tmuxWindow string
		if tmuxPaneID != "" {
			cmd := exec.Command("tmux", "display-message", "-p", "-F", "#S #I", "-t", tmuxPaneID)
			if out, err := cmd.Output(); err == nil {
				parts := strings.Fields(strings.TrimSpace(string(out)))
				if len(parts) >= 2 {
					tmuxSession = parts[0]
					tmuxWindow = parts[1]
				}
			}
		}

		// Map HivemindEvent
		evt := daemon.HivemindEvent{
			Type:      "event",
			EventID:   "evt_" + generateRandomHex(8),
			SessionID: input.ConversationID,
			Timestamp: time.Now().UTC(),
			Context: daemon.EventContext{
				TmuxPaneId:  tmuxPaneID,
				TmuxSession: tmuxSession,
				TmuxWindow:  tmuxWindow,
				Cwd:         cwd,
				GitBranch:   gitBranch,
			},
		}

		// Derive EventType and EventPayload
		switch event {
		case "PreInvocation":
			evt.EventType = "status_changed"
			evt.Payload = daemon.EventPayload{
				Status: daemon.StatusThinking,
			}
		case "PreToolUse":
			evt.EventType = "status_changed"
			status := daemon.StatusToolRunning
			if input.ToolCall != nil {
				if input.ToolCall.Name == "ask_permission" {
					status = daemon.StatusAwaitingPermission
				} else if input.ToolCall.Name == "ask_question" {
					status = daemon.StatusAwaitingInput
				}
			}
			evt.Payload = daemon.EventPayload{
				Status: status,
			}
			if input.ToolCall != nil {
				evt.Payload.Metadata = map[string]interface{}{
					"toolName": input.ToolCall.Name,
					"toolArgs": input.ToolCall.Args,
				}
			}
		case "PostToolUse":
			evt.EventType = "status_changed"
			status := daemon.StatusThinking
			if input.Error != "" {
				status = daemon.StatusErrored
			}
			evt.Payload = daemon.EventPayload{
				Status: status,
			}
			if input.Error != "" {
				evt.Payload.Metadata = map[string]interface{}{
					"errorMessage": input.Error,
				}
			}
		case "Stop":
			if input.FullyIdle {
				evt.EventType = "session_stopped"
				evt.Payload = daemon.EventPayload{
					Status: daemon.StatusIdle,
				}
			} else {
				evt.EventType = "status_changed"
				evt.Payload = daemon.EventPayload{
					Status: daemon.StatusIdle,
				}
			}
		default:
			evt.EventType = "status_changed"
			evt.Payload = daemon.EventPayload{
				Status: daemon.StatusIdle,
			}
		}

		// Send over UDS
		sendUDSEvent(customUdsPath, evt)
	}

	// 4. Respond to stdout
	respondToAntigravity(event)
}

func generateRandomHex(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "abcd1234"
	}
	return hex.EncodeToString(bytes)
}

func sendUDSEvent(customUdsPath string, event daemon.HivemindEvent) {
	socketPath := customUdsPath
	if socketPath == "" {
		socketPath = client.ResolvePath("~/.config/hivemind/hivemind.sock")
	}
	resolvedPath := adapters.ResolveSocketPath(socketPath)

	payloadBytes, err := json.Marshal(event)
	if err != nil {
		return
	}
	payloadBytes = append(payloadBytes, '\n')

	// Non-blocking, fast connections
	for _, path := range []string{resolvedPath, "/tmp/hivemind.sock"} {
		conn, err := net.DialTimeout("unix", path, 50*time.Millisecond)
		if err == nil {
			_, _ = conn.Write(payloadBytes)
			_ = conn.Close()
			return
		}
	}
}

func respondToAntigravity(event string) {
	var response map[string]interface{}
	switch event {
	case "PreToolUse":
		response = map[string]interface{}{
			"decision": "allow",
			"reason":   "Hivemind hook automatically allowing tool call.",
		}
	case "PostToolUse":
		response = map[string]interface{}{}
	case "PreInvocation":
		response = map[string]interface{}{
			"injectSteps": []interface{}{},
		}
	case "PostInvocation":
		response = map[string]interface{}{
			"injectSteps":         []interface{}{},
			"terminationBehavior": "",
		}
	case "Stop":
		response = map[string]interface{}{
			"decision": "allow",
			"reason":   "Hivemind hook automatically allowing stop.",
		}
	default:
		response = map[string]interface{}{}
	}

	outBytes, err := json.Marshal(response)
	if err == nil {
		_, _ = os.Stdout.Write(outBytes)
	}
}

func printHelp() {
	helpText := `hivemind - tmux-native TUI dashboard for AI agent swarms

Usage:
  hivemind                         Start the dashboard TUI client (auto-spawns daemon in background)
  hivemind daemon                  Start the telemetry state daemon in the foreground
  hivemind install-hooks           Install the Python telemetry hooks to AGY plugins directory
  hivemind hook <event>            Process and forward active Antigravity 2.0 hooks
  hivemind run-mock-file-emitter   Run the mock file emitter to simulate transcripts or JSON states

Options:
  -demo                            Run TUI in offline interactive demo mode with mock data
  -restart                         Restart the background daemon if it is already running
  -uds <path>                      Custom Unix Domain Socket path (default: ~/.config/hivemind/hivemind.sock)
  -enabled-tools <list>            Comma-separated list of active tools (all, uds, mock-file, antigravity) (default: all)
  -antigravity-dir <path>          Custom path to search for Antigravity transcripts (default: ~/.gemini/antigravity-cli/brain)
  -sessions-dir <path>             Custom path for mock session JSON files (default: ~/.config/hivemind/sessions)
`
	fmt.Print(helpText)
}

func killExistingDaemon() {
	pidFile := client.ResolvePath("~/.config/hivemind/daemon.pid")
	data, err := os.ReadFile(pidFile)
	if err == nil {
		var pid int
		if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil {
			proc, err := os.FindProcess(pid)
			if err == nil {
				fmt.Printf("Stopping existing daemon (PID %d)...\n", pid)
				// Send SIGTERM
				_ = proc.Signal(syscall.SIGTERM)

				// Wait for it to exit
				exited := false
				for i := 0; i < 10; i++ {
					// Check if process is still running by sending signal 0
					if err := proc.Signal(syscall.Signal(0)); err != nil {
						exited = true
						break
					}
					time.Sleep(100 * time.Millisecond)
				}

				if !exited {
					// Force kill
					fmt.Printf("Force killing daemon (PID %d)...\n", pid)
					_ = proc.Kill()
				}
			}
		}
	}
}
