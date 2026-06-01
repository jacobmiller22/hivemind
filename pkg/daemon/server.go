package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var conversationIDRegex = regexp.MustCompile(`(?i)"conversationId"\s*:\s*"([a-fA-F0-9\-]+)"`)


// Server implements the Hivemind state daemon.
type Server struct {
	State              *StateTree
	StateMu            sync.Mutex
	Subscribers        map[chan []byte]bool
	SubscribersMu      sync.Mutex

	// New Tool Adapter Fields
	Adapters           []ToolAdapter

	// Configuration for cool-off windows
	SubagentCoolOff time.Duration
	SessionCoolOff  time.Duration

	// Pluggable tmux poller function for easy testing
	ListPanesFunc func() ([]string, error)
}

// NewServer initializes a new Server with default settings.
func NewServer() *Server {
	return &Server{
		State: &StateTree{
			Sessions: make(map[string]*SessionState),
		},
		Subscribers:      make(map[chan []byte]bool),
		SubagentCoolOff:  30 * time.Second,
		SessionCoolOff:   30 * time.Second,
		ListPanesFunc:    DefaultListPanes,
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



// Start runs the server's main UDS acceptance loop and the tmux polling ticker.
func (s *Server) Start(ctx context.Context) error {
	// Start background tmux polling and pruning routine
	go s.StartTicker(ctx, 2*time.Second)

	// Start each registered adapter in its own goroutine
	for _, adapter := range s.Adapters {
		go func(a ToolAdapter) {
			_ = a.Start(ctx, s)
		}(adapter)
	}

	<-ctx.Done()
	return nil
}

// HandleConnection handles a single client or hook adapter connection.
func (s *Server) HandleConnection(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)

	var isSubscriber bool
	var subChan chan []byte

	defer func() {
		if isSubscriber {
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

		// Check if it's a subscribe request or a telemetry event
		var msg struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.Type == "subscribe" {
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
			continue
		}

		s.processEvent(&event)
	}
}

// processEvent applies a lifecycle event to the session state tree.
func (s *Server) processEvent(event *HivemindEvent) {
	s.StateMu.Lock()
	defer s.StateMu.Unlock()

	if event.SessionID == "" {
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

	s.broadcastStateLocked()
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
			s.SyncTmuxAndPrune()
		}
	}
}

// SyncTmuxAndPrune polls tmux, marks stale panes as completed, and handles session and subagent cool-offs.
func (s *Server) SyncTmuxAndPrune() {
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
		s.broadcastStateLocked()
	}
}



// broadcastStateLocked serializes and streams the updated StateTree to all open subscribers.
func (s *Server) broadcastStateLocked() {
	stateJSON, err := json.Marshal(s.State)
	if err != nil {
		return
	}

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
func (s *Server) BroadcastState() {
	s.StateMu.Lock()
	defer s.StateMu.Unlock()
	s.broadcastStateLocked()
}

// Emit injects an externally generated HivemindEvent into the state engine.
func (s *Server) Emit(event *HivemindEvent) {
	s.processEvent(event)
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


