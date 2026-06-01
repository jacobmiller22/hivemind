package daemon

import (
	"context"
	"os"
	"time"
)

// ToolAdapter defines the interface that all developer-tool telemetry adapters must implement.
type ToolAdapter interface {
	Name() string
	Start(ctx context.Context, s *Server) error
}

// GenericUDSAdapter ingests direct JSON lifecycle events over a Unix Domain Socket (UDS).
type GenericUDSAdapter struct{}

func (a *GenericUDSAdapter) Name() string {
	return "uds"
}

func (a *GenericUDSAdapter) Start(ctx context.Context, s *Server) error {
	l, resolvedPath, err := ListenUDS(s.SocketPath, s.FallbackSocketPath)
	if err != nil {
		return err
	}
	defer l.Close()
	defer func() {
		_ = os.Remove(resolvedPath)
	}()

	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go s.handleConnection(conn)
	}
}

// MockFileAdapter tail/polls mock session state JSON files.
type MockFileAdapter struct{}

func (a *MockFileAdapter) Name() string {
	return "mock-file"
}

func (a *MockFileAdapter) Start(ctx context.Context, s *Server) error {
	_ = os.MkdirAll(s.SessionsDir, 0755)

	ticker := time.NewTicker(s.FilePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.SyncFileSessions()
		}
	}
}

// AntigravityAdapter tail/polls active Google Antigravity transcript.jsonl log files.
type AntigravityAdapter struct{}

func (a *AntigravityAdapter) Name() string {
	return "antigravity"
}

func (a *AntigravityAdapter) Start(ctx context.Context, s *Server) error {
	if s.AntigravityDir == "" {
		return nil
	}
	_ = os.MkdirAll(s.AntigravityDir, 0755)

	ticker := time.NewTicker(s.FilePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.SyncAntigravitySessions()
		}
	}
}
