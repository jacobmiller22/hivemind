package daemon

import (
	"context"
)

// ToolAdapter defines the interface that all developer-tool telemetry adapters must implement.
type ToolAdapter interface {
	Name() string
	Start(ctx context.Context, s *Server) error
}
