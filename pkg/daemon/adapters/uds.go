package adapters

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacobmiller22/hivemind/pkg/daemon"
)

// GenericUDSAdapter ingests direct JSON lifecycle events over a Unix Domain Socket (UDS).
type GenericUDSAdapter struct {
	SocketPath string
}

func NewGenericUDSAdapter(socketPath string) *GenericUDSAdapter {
	return &GenericUDSAdapter{
		SocketPath: socketPath,
	}
}

func (a *GenericUDSAdapter) Name() string {
	return "uds"
}

func (a *GenericUDSAdapter) Start(ctx context.Context, s *daemon.Server) error {
	l, resolvedPath, err := ListenUDS(a.SocketPath)
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
		go s.HandleConnection(conn)
	}
}

// ListenUDS sets up the Unix Domain Socket listener.
func ListenUDS(socketPath string) (net.Listener, string, error) {
	resolvedPath := ResolveSocketPath(socketPath)

	// Attempt to create the parent directories
	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, "", err
	}

	// Remove existing socket file if it exists
	_ = os.Remove(resolvedPath)

	log.Printf("Listening on socket at %s", resolvedPath)
	l, err := net.Listen("unix", resolvedPath)
	if err != nil {
		return nil, "", err
	}

	return l, resolvedPath, nil
}

// ResolveSocketPath expands the home directory tilde if present.
func ResolveSocketPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
