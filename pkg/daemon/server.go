package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jacobmiller22/gossentials/clog"
)

var conversationIDRegex = regexp.MustCompile(`(?i)"conversationId"\s*:\s*"([a-fA-F0-9\-]+)"`)

// Server implements the Hivemind state daemon.
type Server struct {
	State         *StateTree
	StateMu       sync.Mutex
	Subscribers   map[chan []byte]bool
	SubscribersMu sync.Mutex

	// Configuration for cool-off windows
	SubagentCoolOff time.Duration
	SessionCoolOff  time.Duration

	// Pluggable tmux poller function for easy testing
	ListPanesFunc func() ([]string, error)

	// Custom path to the Unix Domain Socket
	SocketPath string
}

// NewServer initializes a new Server with default settings.
func NewServer() *Server {
	return &Server{
		State: &StateTree{
			Sessions: make(map[string]*SessionState),
		},
		Subscribers:     make(map[chan []byte]bool),
		SubagentCoolOff: 30 * time.Second,
		SessionCoolOff:  30 * time.Second,
		ListPanesFunc:   DefaultListPanes,
	}
}

// DefaultListPanes queries tmux for all active pane IDs.
func DefaultListPanes() ([]string, error) {
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	var panes []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			panes = append(panes, trimmed)
		}
	}
	return panes, nil
}

// ListenUDS sets up the Unix Domain Socket listener.
func ListenUDS(ctx context.Context, socketPath string) (net.Listener, string, error) {
	resolvedPath := ResolveSocketPath(socketPath)

	// Attempt to create the parent directories
	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, "", err
	}

	// Remove existing socket file if it exists
	_ = os.Remove(resolvedPath)

	clog.FromContext(ctx).InfoContext(ctx, "Listening on socket", "path", resolvedPath)
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

// Start runs the server's main UDS acceptance loop and the tmux polling ticker.
func (s *Server) Start(ctx context.Context) error {
	// Start background tmux polling and pruning routine
	go s.StartTicker(ctx, 2*time.Second)

	if s.SocketPath == "" {
		s.SocketPath = "~/.config/hivemind/hivemind.sock"
	}

	l, resolvedPath, err := ListenUDS(ctx, s.SocketPath)
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
		go s.HandleConnection(ctx, conn)
	}
}

// HandleConnection handles a single client or hook adapter connection.
func (s *Server) HandleConnection(ctx context.Context, conn net.Conn) {
	l := clog.FromContext(ctx)
	l.DebugContext(ctx, "HandleConnection: Accepted new connection", "remote", conn.RemoteAddr())
	defer func() {
		l.DebugContext(ctx, "HandleConnection: Closing connection", "remote", conn.RemoteAddr())
		conn.Close()
	}()
	scanner := bufio.NewScanner(conn)

	var isSubscriber bool
	var subChan chan []byte

	defer func() {
		if isSubscriber {
			l.DebugContext(ctx, "HandleConnection: Unsubscribing client", "remote", conn.RemoteAddr())
			s.SubscribersMu.Lock()
			delete(s.Subscribers, subChan)
			s.SubscribersMu.Unlock()
		}
	}()

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		l.DebugContext(ctx, "HandleConnection: Received payload", "payload", string(line))

		// Check if it's a subscribe request or a telemetry event
		var msg struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			l.DebugContext(ctx, "HandleConnection: JSON unmarshal error (preliminary check)", "error", err)
			continue
		}

		if msg.Type == "subscribe" {
			l.DebugContext(ctx, "HandleConnection: Client subscribing", "remote", conn.RemoteAddr())
			isSubscriber = true
			subChan = make(chan []byte, 100)

			s.SubscribersMu.Lock()
			s.Subscribers[subChan] = true
			s.SubscribersMu.Unlock()

			// Send current aggregated state tree immediately
			s.StateMu.Lock()
			stateJSON, err := json.Marshal(s.State)
			s.StateMu.Unlock()
			if err == nil {
				_, _ = conn.Write(append(stateJSON, '\n'))
			}

			// Background loop to stream state updates to this subscriber
			go func() {
				for data := range subChan {
					_, err := conn.Write(append(data, '\n'))
					if err != nil {
						l.DebugContext(ctx, "HandleConnection: Subscription write error", "remote", conn.RemoteAddr(), "error", err)
						_ = conn.Close()
						return
					}
				}
			}()

			continue
		}

		// Otherwise, process as a HivemindEvent
		var event HivemindEvent
		if err := json.Unmarshal(line, &event); err != nil {
			l.DebugContext(ctx, "HandleConnection: JSON unmarshal error (HivemindEvent)", "error", err)
			continue
		}

		l.DebugContext(ctx, "HandleConnection: Processing event", "type", event.Type, "sessionId", event.SessionID, "eventType", event.EventType)
		s.processEvent(ctx, &event)
	}
}

// processEvent applies a lifecycle event to the session state tree.
func (s *Server) processEvent(ctx context.Context, event *HivemindEvent) {
	l := clog.FromContext(ctx)
	l.DebugContext(ctx, "processEvent", "sessionId", event.SessionID, "eventType", event.EventType, "status", event.Payload.Status)
	s.StateMu.Lock()
	defer s.StateMu.Unlock()

	if event.SessionID == "" {
		l.DebugContext(ctx, "processEvent: SessionID is empty, skipping")
		return
	}

	now := time.Now()

	// Get or initialize the parent session
	session, exists := s.State.Sessions[event.SessionID]
	if !exists {
		session = &SessionState{
			SessionID: event.SessionID,
			Subagents: make(map[string]*Subagent),
		}
		s.State.Sessions[event.SessionID] = session
	}

	// Update telemetry context coordinates
	if event.Context.TmuxPaneId != "" {
		session.TmuxPaneID = event.Context.TmuxPaneId
	}
	if event.Context.TmuxSession != "" {
		session.TmuxSession = event.Context.TmuxSession
	}
	if event.Context.TmuxWindow != "" {
		session.TmuxWindow = event.Context.TmuxWindow
	}
	if event.Context.Cwd != "" {
		session.Cwd = event.Context.Cwd
	}
	if event.Context.GitBranch != "" {
		session.GitBranch = event.Context.GitBranch
	}

	session.LastEventReceived = now
	if !event.Timestamp.IsZero() {
		session.LastActivity = event.Timestamp
	} else {
		session.LastActivity = now
	}

	switch event.EventType {
	case "session_started":
		session.Status = StatusIdle
		if event.Payload.Status != "" {
			session.Status = event.Payload.Status
		}
		if event.Payload.Model != "" {
			session.Model = event.Payload.Model
		}
		session.PaneExited = false
		session.PaneExitedAt = nil

	case "status_changed":
		if event.Payload.Status != "" {
			session.Status = event.Payload.Status
		}
		if event.Payload.Model != "" {
			session.Model = event.Payload.Model
		}

	case "subagent_spawned":
		if event.Payload.Subagent != nil {
			saPayload := event.Payload.Subagent
			status := SubagentRunning
			if saPayload.Status != "" {
				status = SubagentStatus(saPayload.Status)
			}

			sa := &Subagent{
				ID:        saPayload.ID,
				Role:      saPayload.Role,
				TypeName:  saPayload.TypeName,
				Status:    status,
				SpawnedAt: session.LastActivity,
			}
			if status == SubagentCompleted || status == SubagentErrored {
				sa.CompletedAt = &session.LastActivity
			}
			session.Subagents[saPayload.ID] = sa
		}

	case "subagent_status_changed":
		if event.Payload.Subagent != nil {
			saPayload := event.Payload.Subagent
			sa, saExists := session.Subagents[saPayload.ID]
			if !saExists {
				sa = &Subagent{
					ID:        saPayload.ID,
					Role:      saPayload.Role,
					TypeName:  saPayload.TypeName,
					SpawnedAt: session.LastActivity,
				}
				session.Subagents[saPayload.ID] = sa
			}
			sa.Status = SubagentStatus(saPayload.Status)
			if sa.Status == SubagentCompleted || sa.Status == SubagentErrored {
				completedAt := session.LastActivity
				sa.CompletedAt = &completedAt
			}
		}

	case "session_stopped":
		session.Status = StatusCompleted
		session.PaneExited = true
		session.PaneExitedAt = &now
	}

	s.broadcastStateLocked(ctx)
}

// StartTicker runs the periodic tmux pane checker.
func (s *Server) StartTicker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SyncTmuxAndPrune(ctx)
		}
	}
}

// SyncTmuxAndPrune polls tmux, marks stale panes as completed, and handles session and subagent cool-offs.
func (s *Server) SyncTmuxAndPrune(ctx context.Context) {
	s.StateMu.Lock()
	defer s.StateMu.Unlock()

	now := time.Now()

	// 1. Fetch active panes from tmux
	activePanes := make(map[string]bool)
	if s.ListPanesFunc != nil {
		panes, err := s.ListPanesFunc()
		if err == nil {
			for _, p := range panes {
				activePanes[p] = true
			}
		}
	}

	stateChanged := false

	// 2. Iterate and update state
	for id, session := range s.State.Sessions {
		// A. Check if pane is still listed in tmux
		if session.TmuxPaneID != "" {
			if !activePanes[session.TmuxPaneID] {
				// Pane closed in tmux
				if !session.PaneExited {
					session.PaneExited = true
					exitedAt := now
					session.PaneExitedAt = &exitedAt
					session.Status = StatusCompleted
					stateChanged = true
				}
			} else {
				// Pane active. If telemetry has timed out (e.g. 10s of no events), degrade status
				if session.Status != StatusCompleted && session.Status != StatusErrored && session.Status != StatusNoTelemetry {
					if now.Sub(session.LastEventReceived) > 10*time.Second {
						session.Status = StatusNoTelemetry
						stateChanged = true
					}
				}
			}
		}

		// B. Prune completed sessions after cool-off
		if session.PaneExited && session.PaneExitedAt != nil {
			if now.Sub(*session.PaneExitedAt) > s.SessionCoolOff {
				delete(s.State.Sessions, id)
				stateChanged = true
				continue
			}
		}

		// C. Prune subagents after cool-off
		for saID, sa := range session.Subagents {
			if sa.Status == SubagentCompleted || sa.Status == SubagentErrored {
				if sa.CompletedAt != nil && now.Sub(*sa.CompletedAt) > s.SubagentCoolOff {
					delete(session.Subagents, saID)
					stateChanged = true
				}
			}
		}
	}

	if stateChanged {
		s.broadcastStateLocked(ctx)
	}
}

// broadcastStateLocked serializes and streams the updated StateTree to all open subscribers.
func (s *Server) broadcastStateLocked(ctx context.Context) {
	stateJSON, err := json.Marshal(s.State)
	if err != nil {
		return
	}

	clog.FromContext(ctx).DebugContext(ctx, "broadcastStateLocked: Broadcasting state", "subscribers", len(s.Subscribers))

	s.SubscribersMu.Lock()
	defer s.SubscribersMu.Unlock()

	for subChan := range s.Subscribers {
		select {
		case subChan <- stateJSON:
		default:
			// Non-blocking write to avoid lagging subscribers blocking the daemon
		}
	}
}

// GetState returns a thread-safe copy/reference to the StateTree (useful for testing).
func (s *Server) GetState() *StateTree {
	s.StateMu.Lock()
	defer s.StateMu.Unlock()
	return s.State
}

// Close closes all active subscriber connections.
func (s *Server) Close() error {
	s.SubscribersMu.Lock()
	defer s.SubscribersMu.Unlock()

	for subChan := range s.Subscribers {
		close(subChan)
		delete(s.Subscribers, subChan)
	}
	return nil
}

// BroadcastState serializes and streams the updated StateTree to all open subscribers.
func (s *Server) BroadcastState(ctx context.Context) {
	s.StateMu.Lock()
	defer s.StateMu.Unlock()
	s.broadcastStateLocked(ctx)
}

// Emit injects an externally generated HivemindEvent into the state engine.
func (s *Server) Emit(ctx context.Context, event *HivemindEvent) {
	s.processEvent(ctx, event)
}

func SubagentsEqual(a, b map[string]*Subagent) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		vb, exists := b[k]
		if !exists {
			return false
		}
		if v.ID != vb.ID || v.Role != vb.Role || v.TypeName != vb.TypeName || v.Status != vb.Status {
			return false
		}
		if !v.SpawnedAt.Equal(vb.SpawnedAt) {
			return false
		}
		if (v.CompletedAt == nil) != (vb.CompletedAt == nil) {
			return false
		}
		if v.CompletedAt != nil && vb.CompletedAt != nil && !v.CompletedAt.Equal(*vb.CompletedAt) {
			return false
		}
	}
	return true
}
