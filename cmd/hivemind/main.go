package main

import (
	"context"
	"flag"
	"fmt"
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

func printHelp() {
	helpText := `hivemind - tmux-native TUI dashboard for AI agent swarms

Usage:
  hivemind                         Start the dashboard TUI client (auto-spawns daemon in background)
  hivemind daemon                  Start the telemetry state daemon in the foreground
  hivemind install-hooks           Install the Python telemetry hooks to AGY plugins directory
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
