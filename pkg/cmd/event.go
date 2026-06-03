package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jacobmiller22/gossentials/clog"
	"github.com/jacobmiller22/hivemind/pkg/client"
	"github.com/jacobmiller22/hivemind/pkg/config"
	"github.com/jacobmiller22/hivemind/pkg/daemon"
	"github.com/jacobmiller22/hivemind/pkg/daemon/adapters"
	"github.com/jacobmiller22/hivemind/pkg/logkeys"
)

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

func Event(ctx context.Context, args []string) error {
	l := clog.FromContext(ctx)
	cfg := config.LoadConfig(args)

	l.Debug(logkeys.CommandStart, logkeys.Command, "HIVEMIND_EVENT", logkeys.Config, cfg)

	if len(args) < 1 {
		err := fmt.Errorf("missing event name")
		l.ErrorContext(ctx, "Argument error", logkeys.Error, err)
		return err
	}
	event := args[0]

	// Read stdin
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		l.ErrorContext(ctx, "Error reading stdin", logkeys.Error, err)
		respondToAntigravity(event)
		return nil
	}

	var input AntigravityInput
	if len(stdinData) > 0 {
		if err := json.Unmarshal(stdinData, &input); err != nil {
			l.ErrorContext(ctx, "Error parsing JSON input", logkeys.Error, err)
			respondToAntigravity(event)
			return nil
		}
	}

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

		l.DebugContext(ctx, "Sending event to UDS daemon", "eventType", evt.EventType, "sessionId", evt.SessionID)

		// Send over UDS
		sendUDSEvent(cfg.SocketPath, evt)
	}

	respondToAntigravity(event)
	return nil
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
