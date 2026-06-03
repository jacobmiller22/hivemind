package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jacobmiller22/gossentials/clog"
	"github.com/jacobmiller22/hivemind/pkg/client"
	"github.com/jacobmiller22/hivemind/pkg/config"
	"github.com/jacobmiller22/hivemind/pkg/logkeys"
)

func Client(ctx context.Context, args []string) error {
	l := clog.FromContext(ctx)
	cfg := config.LoadConfig(args)

	l.Debug(logkeys.CommandStart, logkeys.Command, "HIVEMIND_CLIENT", logkeys.Config, cfg)

	if cfg.RestartDaemon {
		l.DebugContext(ctx, "restarting daemon")
		killExistingDaemon(ctx, cfg.Server.PidFilePath)
	}

	udsPath := client.ResolvePath(cfg.SocketPath)

	demo := false
	for _, arg := range args {
		if arg == "-demo" || arg == "--demo" {
			demo = true
			break
		}
	}

	if !demo {
		conn, err := net.DialTimeout("unix", udsPath, 100*time.Millisecond)
		if err != nil {
			l.WarnContext(ctx, "Error dialing unix socket, attempting to spawn daemon", logkeys.Error, err)
			if err := spawnDaemon(ctx, cfg.SocketPath); err != nil {
				l.ErrorContext(ctx, "Failed to spawn daemon", logkeys.Error, err)
				return err
			}
			// Redial after spawning
			conn, err = net.DialTimeout("unix", udsPath, 100*time.Millisecond)
			if err != nil {
				l.ErrorContext(ctx, "Failed to connect to spawned daemon", logkeys.Error, err)
				return err
			}
		}
		if conn != nil {
			conn.Close()
		}
	}

	m := client.NewModel(udsPath, demo)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		l.ErrorContext(ctx, "Error running TUI program", logkeys.Error, err)
		return err
	}

	return nil
}

func spawnDaemon(ctx context.Context, socketPath string) error {

	l := clog.FromContext(ctx)

	args := []string{"./hivemind", "daemon"}

	l.WarnContext(ctx, "Daemon not running. Auto-spawning background daemon...\n", "cmd", args)

	cmd := exec.Command(args[0], args[1:]...)

	if err := cmd.Start(); err != nil {
		l.WarnContext(ctx, "Failed to auto-spawn daemon: %v\n", logkeys.Error, err)
		return err
	} else {
		// Poll the UNIX domain socket after resolving home directory tilde
		start := time.Now()
		timeout := 2000 * time.Millisecond
		connected := false
		resolvedPath := client.ResolvePath(socketPath)
		for time.Since(start) < timeout {
			conn, err := net.DialTimeout("unix", resolvedPath, 50*time.Millisecond)
			if err == nil {
				conn.Close()
				connected = true
				break
			}
			time.Sleep(30 * time.Millisecond)
		}
		if !connected {
			l.WarnContext(ctx, "Daemon auto-spawned, but UNIX domain socket did not become ready after timeout", "timeout", timeout)
			return fmt.Errorf("did not connect to spawned daemon") // TODO: Custom error
		}
	}

	return nil
}

func killExistingDaemon(ctx context.Context, daemonPidFilePath string) {
	l := clog.FromContext(ctx)

	pidFile := client.ResolvePath(daemonPidFilePath)
	data, err := os.ReadFile(pidFile)
	if err == nil {
		var pid int
		if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil {
			proc, err := os.FindProcess(pid)
			if err == nil {
				l.InfoContext(ctx, "Stopping existing daemon", "pid", pid)
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
					l.InfoContext(ctx, "Force killing daemon", "pid", pid)
					_ = proc.Kill()
				}
			}
		}
	}
}
