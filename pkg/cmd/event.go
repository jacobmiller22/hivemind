package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jacobmiller22/gossentials/clog"
	"github.com/jacobmiller22/hivemind/pkg/client"
	"github.com/jacobmiller22/hivemind/pkg/config"
	"github.com/jacobmiller22/hivemind/pkg/daemon"
	"github.com/jacobmiller22/hivemind/pkg/logkeys"
	"github.com/jacobmiller22/hivemind/pkg/plugins"
	_ "github.com/jacobmiller22/hivemind/pkg/plugins/antigravity"
)

func Event(ctx context.Context, args []string) error {
	l := clog.FromContext(ctx)
	cfg := config.LoadConfig(args)

	l.DebugContext(ctx, logkeys.CommandStart, logkeys.Command, "HIVEMIND_EVENT", logkeys.Config, cfg)

	if len(args) < 2 {
		err := fmt.Errorf("missing plugin or event name. Usage: hivemind event <plugin> <event>")
		l.ErrorContext(ctx, "Argument error", logkeys.Error, err)
		return err
	}
	pluginName := args[0]
	eventName := args[1]

	p, exists := plugins.Get(pluginName)
	if !exists {
		err := fmt.Errorf("unknown plugin: %s", pluginName)
		l.ErrorContext(ctx, "Plugin error", logkeys.Error, err)
		return err
	}

	evt, err := p.HandleEvent(ctx, eventName, os.Stdin, os.Stdout)
	if err != nil {
		l.ErrorContext(ctx, "Plugin handle event error", logkeys.Error, err)
		return err
	}

	if evt != nil {
		l.DebugContext(ctx, "Sending event to UDS daemon", "eventType", evt.EventType, "sessionId", evt.SessionID)
		sendUDSEvent(cfg.SocketPath, *evt)
	}

	return nil
}

func sendUDSEvent(customUdsPath string, event daemon.HivemindEvent) {
	socketPath := customUdsPath
	if socketPath == "" {
		socketPath = client.ResolvePath("~/.config/hivemind/hivemind.sock")
	}
	resolvedPath := daemon.ResolveSocketPath(socketPath)

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
