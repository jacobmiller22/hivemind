package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/jacobmiller22/gossentials/clog"
	"github.com/jacobmiller22/hivemind/pkg/client"
	"github.com/jacobmiller22/hivemind/pkg/config"
	"github.com/jacobmiller22/hivemind/pkg/daemon"
	"github.com/jacobmiller22/hivemind/pkg/logkeys"
)

func Daemon(ctx context.Context, args []string) error {
	l := clog.FromContext(ctx)
	cfg := config.LoadConfig(args)

	l.Debug(logkeys.CommandStart, logkeys.Command, "HIVEMIND_DAEMON", logkeys.Config, cfg)

	s := daemon.NewServer()
	s.SocketPath = cfg.SocketPath

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// ~/.config/hivemind/daemon.pid
	pidFile := client.ResolvePath(cfg.Server.PidFilePath)
	_ = os.MkdirAll(filepath.Dir(pidFile), 0755)
	_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
	defer os.Remove(pidFile)

	if err := s.Start(ctx); err != nil {
		l.ErrorContext(ctx, "DAEMON_ERROR", logkeys.Error, err)
		os.Exit(1)
	}
	l.InfoContext(ctx, "DAEMON_STOPPED")

	return nil
}

// func runDaemon(customUdsPath, enabledTools, customAntigravityDir, customSessionsDir, filePoll string) {

// 	// socketPath := customUdsPath
// 	// if socketPath == "" {
// 	// 	socketPath = client.ResolvePath("~/.config/hivemind/hivemind.sock")
// 	// }
// 	// fallbackPath := "/tmp/hivemind.sock"

// 	// sessionsDir := customSessionsDir
// 	// if sessionsDir == "" {
// 	// 	sessionsDir = client.ResolvePath("~/.config/hivemind/sessions")
// 	// } else {
// 	// 	sessionsDir = client.ResolvePath(sessionsDir)
// 	// }

// 	// antigravityDir := customAntigravityDir
// 	// if antigravityDir == "" {
// 	// 	antigravityDir = client.ResolvePath("~/.gemini/antigravity-treecli/brain")
// 	// } else {
// 	// 	antigravityDir = client.ResolvePath(antigravityDir)
// 	// }

// 	s := daemon.NewServer()

// 	pollInterval := 1 * time.Second
// 	if pollDur, err := time.ParseDuration(filePoll); err == nil {
// 		pollInterval = pollDur
// 	}

// 	// Register adapters according to enabledTools config
// 	// enabled := strings.ToLower(enabledTools)
// 	// tools := strings.Split(enabled, ",")
// 	// allMode := enabled == "all" || enabled == ""

// 	// Ensure stable deduplicated check
// 	addedTools := make(map[string]bool)
// 	for _, tool := range tools {
// 		tool = strings.TrimSpace(tool)
// 		if tool == "uds" || allMode {
// 			if !addedTools["uds"] {
// 				s.Adapters = append(s.Adapters, adapters.NewGenericUDSAdapter(socketPath))
// 				addedTools["uds"] = true
// 			}
// 		}
// 		if tool == "mock-file" || allMode {
// 			if !addedTools["mock-file"] {
// 				s.Adapters = append(s.Adapters, adapters.NewMockFileAdapter(sessionsDir, pollInterval))
// 				addedTools["mock-file"] = true
// 			}
// 		}
// 		if tool == "antigravity" || allMode {
// 			if !addedTools["antigravity"] {
// 				s.Adapters = append(s.Adapters, adapters.NewAntigravityAdapter(antigravityDir, pollInterval))
// 				addedTools["antigravity"] = true
// 			}
// 		}
// 	}

// 	ctx, cancel := context.WithCancel(context.Background())
// 	defer cancel()

// 	// Handle graceful shutdown signals
// 	sigChan := make(chan os.Signal, 1)
// 	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
// 	go func() {
// 		<-sigChan
// 		cancel()
// 	}()

// 	log.Printf("Starting hivemind daemon...\n")
// 	log.Printf("Primary socket:  %s\n", socketPath)
// 	log.Printf("Fallback socket: %s\n", fallbackPath)
// 	log.Printf("Sessions dir:    %s\n", sessionsDir)
// 	log.Printf("Antigravity dir: %s\n", antigravityDir)

// 	var activeToolNames []string
// 	for _, a := range s.Adapters {
// 		activeToolNames = append(activeToolNames, a.Name())
// 	}
// 	log.Printf("Active tools:    %s\n", strings.Join(activeToolNames, ", "))

// 	pidFile := client.ResolvePath("~/.config/hivemind/daemon.pid")
// 	_ = os.MkdirAll(filepath.Dir(pidFile), 0755)
// 	_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
// 	defer os.Remove(pidFile)

// 	if err := s.Start(ctx); err != nil {
// 		log.Printf("Daemon error: %v\n", err)
// 		os.Exit(1)
// 	}
// 	log.Printf("Daemon stopped cleanly.\n")
// }
