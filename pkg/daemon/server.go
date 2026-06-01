package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Server implements the Hivemind state daemon.
type Server struct {
	SocketPath         string
	SessionsDir        string
	State              *StateTree
	StateMu            sync.Mutex
	Subscribers        map[chan []byte]bool
	SubscribersMu      sync.Mutex

	// New Tool Adapter Fields
	Adapters           []ToolAdapter
	AntigravityDir     string // Configurable brain directory for Antigravity
	FilePollInterval   time.Duration

	// Configuration for cool-off windows
	SubagentCoolOff time.Duration
	SessionCoolOff  time.Duration

	// Pluggable tmux poller function for easy testing
	ListPanesFunc func() ([]string, error)
}

// NewServer initializes a new Server with default settings.
func NewServer(socketPath, sessionsDir string) *Server {
	return &Server{
		SocketPath:         socketPath,
		SessionsDir:        sessionsDir,
		State: &StateTree{
			Sessions: make(map[string]*SessionState),
		},
		Subscribers:      make(map[chan []byte]bool),
		SubagentCoolOff:  30 * time.Second,
		SessionCoolOff:   30 * time.Second,
		ListPanesFunc:    DefaultListPanes,
		FilePollInterval: 1 * time.Second,
	}
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
func ListenUDS(socketPath string) (net.Listener, string, error) {
	resolvedPath := ResolveSocketPath(socketPath)

	// Attempt to create the parent directories
	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, "", err
	}

	// Remove existing socket file if it exists
	_ = os.Remove(resolvedPath)

	l, err := net.Listen("unix", resolvedPath)
	if err != nil {
		return nil, "", err
	}

	return l, resolvedPath, nil
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

// handleConnection handles a single client or hook adapter connection.
func (s *Server) handleConnection(conn net.Conn) {
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

// StartFilePoller runs the periodic JSON file session poller.
func (s *Server) StartFilePoller(ctx context.Context, interval time.Duration) {
	_ = os.MkdirAll(s.SessionsDir, 0755)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SyncFileSessions()
		}
	}
}

// SyncFileSessions reads JSON session files from SessionsDir and merges them into the state tree.
func (s *Server) SyncFileSessions() {
	entries, err := os.ReadDir(s.SessionsDir)
	if err != nil {
		return
	}

	now := time.Now()
	seenIDs := make(map[string]bool)
	stateChanged := false

	type parsedSession struct {
		key     string
		fss     FileSessionState
		modTime time.Time
	}

	var parsed []parsedSession
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(s.SessionsDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var fss FileSessionState
		if err := json.Unmarshal(data, &fss); err != nil {
			continue
		}

		fileInfo, err := entry.Info()
		modTime := now
		if err == nil {
			modTime = fileInfo.ModTime()
		}

		key := "file:" + fss.SessionID
		seenIDs[key] = true
		parsed = append(parsed, parsedSession{key: key, fss: fss, modTime: modTime})
	}

	s.StateMu.Lock()
	defer s.StateMu.Unlock()

	for _, p := range parsed {
		fss := p.fss
		modTime := p.modTime
		session, exists := s.State.Sessions[p.key]
		if exists {
			// Align subagents to avoid spurious mismatch of SpawnedAt / CompletedAt
			for _, sa := range fss.Subagents {
				if oldSa, ok := session.Subagents[sa.ID]; ok {
					sa.SpawnedAt = oldSa.SpawnedAt
					if sa.Status == oldSa.Status {
						sa.CompletedAt = oldSa.CompletedAt
					} else if sa.Status == SubagentCompleted || sa.Status == SubagentErrored {
						if sa.CompletedAt == nil {
							sa.CompletedAt = &modTime
						}
					}
				} else {
					sa.SpawnedAt = modTime
					if sa.Status == SubagentCompleted || sa.Status == SubagentErrored {
						sa.CompletedAt = &modTime
					}
				}
			}

			changed := session.Status != fss.Status ||
				session.Model != fss.Model ||
				session.Cwd != fss.Cwd ||
				session.GitBranch != fss.GitBranch ||
				session.TmuxPaneID != fss.TmuxPaneID ||
				session.TmuxSession != fss.TmuxSession ||
				session.TmuxWindow != fss.TmuxWindow ||
				!subagentsEqual(session.Subagents, fss.Subagents) ||
				session.LastActivity.Before(modTime)

			if changed {
				session.Status = fss.Status
				session.Model = fss.Model
				session.Cwd = fss.Cwd
				session.GitBranch = fss.GitBranch
				session.TmuxPaneID = fss.TmuxPaneID
				session.TmuxSession = fss.TmuxSession
				session.TmuxWindow = fss.TmuxWindow
				session.Subagents = fss.Subagents
				session.LastActivity = modTime
				session.LastEventReceived = modTime
				stateChanged = true
			}
		} else {
			subagents := fss.Subagents
			if subagents == nil {
				subagents = make(map[string]*Subagent)
			}
			// Align subagents for new session
			for _, sa := range subagents {
				sa.SpawnedAt = modTime
				if sa.Status == SubagentCompleted || sa.Status == SubagentErrored {
					sa.CompletedAt = &modTime
				}
			}
			s.State.Sessions[p.key] = &SessionState{
				SessionID:         fss.SessionID,
				TmuxPaneID:        fss.TmuxPaneID,
				TmuxSession:       fss.TmuxSession,
				TmuxWindow:        fss.TmuxWindow,
				Cwd:               fss.Cwd,
				GitBranch:         fss.GitBranch,
				Model:             fss.Model,
				Status:            fss.Status,
				LastActivity:      modTime,
				LastEventReceived: modTime,
				Subagents:         subagents,
			}
			stateChanged = true
		}
	}

	// Prune file-based sessions whose JSON file was deleted
	for id, session := range s.State.Sessions {
		if strings.HasPrefix(id, "file:") {
			jsonPath := filepath.Join(s.SessionsDir, session.SessionID+".json")
			logsDir := filepath.Join(s.AntigravityDir, session.SessionID, ".system_generated", "logs")
			transcriptPath1 := filepath.Join(logsDir, "transcript.jsonl")
			transcriptPath2 := filepath.Join(logsDir, "transcript_full.jsonl")

			_, errJson := os.Stat(jsonPath)
			_, errTrans1 := os.Stat(transcriptPath1)
			_, errTrans2 := os.Stat(transcriptPath2)

			jsonExists := errJson == nil
			transExists := errTrans1 == nil || errTrans2 == nil

			if !jsonExists && !transExists {
				delete(s.State.Sessions, id)
				stateChanged = true
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

// SyncAntigravitySessions scans s.AntigravityDir, parses transcript.jsonl files, and updates the state tree.
func (s *Server) SyncAntigravitySessions() {
	entries, err := os.ReadDir(s.AntigravityDir)
	if err != nil {
		return
	}

	now := time.Now()
	seenIDs := make(map[string]bool)
	stateChanged := false

	type parsedSession struct {
		key     string
		fss     FileSessionState
		modTime time.Time
	}

	var parsed []parsedSession
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		sessionID := entry.Name()
		// Try to find transcript.jsonl or transcript_full.jsonl
		logsDir := filepath.Join(s.AntigravityDir, sessionID, ".system_generated", "logs")
		transcriptPath := filepath.Join(logsDir, "transcript.jsonl")
		if _, err := os.Stat(transcriptPath); err != nil {
			transcriptPath = filepath.Join(logsDir, "transcript_full.jsonl")
			if _, err := os.Stat(transcriptPath); err != nil {
				continue
			}
		}

		info, err := os.Stat(transcriptPath)
		modTime := now
		if err == nil {
			modTime = info.ModTime()
		}

		fss, err := s.parseTranscriptFile(transcriptPath, sessionID)
		if err != nil {
			continue
		}

		key := "file:" + sessionID
		seenIDs[key] = true
		parsed = append(parsed, parsedSession{key: key, fss: *fss, modTime: modTime})
	}

	s.StateMu.Lock()
	defer s.StateMu.Unlock()

	for _, p := range parsed {
		fss := p.fss
		modTime := p.modTime
		session, exists := s.State.Sessions[p.key]
		if exists {
			// Align subagents to avoid spurious mismatch of SpawnedAt / CompletedAt
			for _, sa := range fss.Subagents {
				if oldSa, ok := session.Subagents[sa.ID]; ok {
					sa.SpawnedAt = oldSa.SpawnedAt
					if sa.Status == oldSa.Status {
						sa.CompletedAt = oldSa.CompletedAt
					} else if sa.Status == SubagentCompleted || sa.Status == SubagentErrored {
						if sa.CompletedAt == nil {
							sa.CompletedAt = &modTime
						}
					}
				} else {
					sa.SpawnedAt = modTime
					if sa.Status == SubagentCompleted || sa.Status == SubagentErrored {
						sa.CompletedAt = &modTime
					}
				}
			}

			changed := session.Status != fss.Status ||
				session.Model != fss.Model ||
				session.Cwd != fss.Cwd ||
				session.GitBranch != fss.GitBranch ||
				session.TmuxPaneID != fss.TmuxPaneID ||
				session.TmuxSession != fss.TmuxSession ||
				session.TmuxWindow != fss.TmuxWindow ||
				!subagentsEqual(session.Subagents, fss.Subagents) ||
				session.LastActivity.Before(modTime)

			if changed {
				session.Status = fss.Status
				session.Model = fss.Model
				session.Cwd = fss.Cwd
				session.GitBranch = fss.GitBranch
				session.TmuxPaneID = fss.TmuxPaneID
				session.TmuxSession = fss.TmuxSession
				session.TmuxWindow = fss.TmuxWindow
				session.Subagents = fss.Subagents
				session.LastActivity = modTime
				session.LastEventReceived = modTime
				stateChanged = true
			}
		} else {
			subagents := fss.Subagents
			if subagents == nil {
				subagents = make(map[string]*Subagent)
			}
			// Align subagents for new session
			for _, sa := range subagents {
				sa.SpawnedAt = modTime
				if sa.Status == SubagentCompleted || sa.Status == SubagentErrored {
					sa.CompletedAt = &modTime
				}
			}
			s.State.Sessions[p.key] = &SessionState{
				SessionID:         fss.SessionID,
				TmuxPaneID:        fss.TmuxPaneID,
				TmuxSession:       fss.TmuxSession,
				TmuxWindow:        fss.TmuxWindow,
				Cwd:               fss.Cwd,
				GitBranch:         fss.GitBranch,
				Model:             fss.Model,
				Status:            fss.Status,
				LastActivity:      modTime,
				LastEventReceived: modTime,
				Subagents:         subagents,
			}
			stateChanged = true
		}
	}

	// Prune file-based sessions whose transcripts were deleted
	for id, session := range s.State.Sessions {
		if strings.HasPrefix(id, "file:") {
			jsonPath := filepath.Join(s.SessionsDir, session.SessionID+".json")
			logsDir := filepath.Join(s.AntigravityDir, session.SessionID, ".system_generated", "logs")
			transcriptPath1 := filepath.Join(logsDir, "transcript.jsonl")
			transcriptPath2 := filepath.Join(logsDir, "transcript_full.jsonl")

			_, errJson := os.Stat(jsonPath)
			_, errTrans1 := os.Stat(transcriptPath1)
			_, errTrans2 := os.Stat(transcriptPath2)

			jsonExists := errJson == nil
			transExists := errTrans1 == nil || errTrans2 == nil

			if !jsonExists && !transExists {
				delete(s.State.Sessions, id)
				stateChanged = true
			}
		}
	}

	if stateChanged {
		s.broadcastStateLocked()
	}
}

// TranscriptLine represents a single parsed line of the transcript.jsonl file
type TranscriptLine struct {
	StepIndex int    `json:"step_index"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Content   string `json:"content"`
	ToolCalls []struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"tool_calls"`
}

// parseTranscriptFile reads a transcript JSONL file and reconstructs the session state
func (s *Server) parseTranscriptFile(path string, sessionID string) (*FileSessionState, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []TranscriptLine
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line TranscriptLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err == nil {
			lines = append(lines, line)
		}
	}

	if len(lines) == 0 {
		return nil, fmt.Errorf("empty transcript")
	}

	// Default coordinates
	fss := &FileSessionState{
		SessionID:   sessionID,
		TmuxPaneID:  "%0",
		TmuxSession: "antigravity",
		TmuxWindow:  "1",
		Status:      StatusIdle,
		Subagents:   make(map[string]*Subagent),
	}

	// Heuristics mapping tool calls by step index to track subagents completion
	subagentsByStep := make(map[int][]string)

	for _, line := range lines {
		// Extract CWD from first user input
		if line.Type == "USER_INPUT" && line.Content != "" {
			if cwd := extractCwd(line.Content); cwd != "" {
				fss.Cwd = cwd
			}
			if model := extractModel(line.Content); model != "" {
				fss.Model = model
			}
		}

		// Extract spawned subagents
		if line.Type == "PLANNER_RESPONSE" && len(line.ToolCalls) > 0 {
			for _, tc := range line.ToolCalls {
				if tc.Name == "invoke_subagent" {
					var args struct {
						Subagents []struct {
							Role     string `json:"Role"`
							TypeName string `json:"TypeName"`
						} `json:"Subagents"`
					}
					
					// Robust dual-unmarshaling to support both raw JSON objects and double-serialized strings
					err := json.Unmarshal(tc.Args, &args)
					if err != nil {
						var argsStr string
						if json.Unmarshal(tc.Args, &argsStr) == nil {
							_ = json.Unmarshal([]byte(argsStr), &args)
						}
					}

					for idx, sa := range args.Subagents {
						saID := fmt.Sprintf("sub_%d_%d", line.StepIndex, idx)
						subagentsByStep[line.StepIndex] = append(subagentsByStep[line.StepIndex], saID)
						fss.Subagents[saID] = &Subagent{
							ID:        saID,
							Role:      sa.Role,
							TypeName:  sa.TypeName,
							Status:    SubagentRunning,
							SpawnedAt: time.Now(),
						}
					}
				}
			}
		}

		// Mark subagents as completed when the INVOKE_SUBAGENT tool execution returns
		if line.Type == "INVOKE_SUBAGENT" && line.Status == "DONE" {
			// Find the parent planner response that spawned it (usually previous steps)
			// For robustness, we check the spawned subagents from previous steps and mark them as completed
			for stepIdx, saIDs := range subagentsByStep {
				if stepIdx < line.StepIndex {
					for _, saID := range saIDs {
						if sa, exists := fss.Subagents[saID]; exists && sa.Status == SubagentRunning {
							sa.Status = SubagentCompleted
							completedAt := time.Now()
							sa.CompletedAt = &completedAt
						}
					}
				}
			}
		}
	}

	// Apply status heuristic to the very last line
	lastLine := lines[len(lines)-1]
	switch lastLine.Type {
	case "USER_INPUT":
		fss.Status = StatusThinking
	case "PLANNER_RESPONSE":
		if lastLine.Status != "DONE" {
			fss.Status = StatusThinking
		} else {
			if len(lastLine.ToolCalls) > 0 {
				tc := lastLine.ToolCalls[0]
				switch tc.Name {
				case "ask_permission":
					fss.Status = StatusAwaitingPermission
				case "ask_question":
					fss.Status = StatusAwaitingInput
				default:
					fss.Status = StatusToolRunning
				}
			} else {
				fss.Status = StatusIdle
			}
		}
	default:
		// Tool execution steps
		if lastLine.Status != "DONE" {
			fss.Status = StatusToolRunning
		} else {
			fss.Status = StatusThinking // Tool just outputted; model about to plan/think
		}
	}

	return fss, nil
}

// extractCwd pulls Cwd path from <user_information> block in the transcript content
func extractCwd(content string) string {
	lines := strings.Split(content, "\n")
	for _, l := range lines {
		if strings.Contains(l, "->") {
			parts := strings.Split(l, "->")
			if len(parts) > 0 {
				trimmed := strings.TrimSpace(parts[0])
				if strings.HasPrefix(trimmed, "/") {
					return trimmed
				}
			}
		}
	}
	return ""
}

// extractModel parses the Model Selection setting change in transcript content
func extractModel(content string) string {
	if idx := strings.Index(content, "Model Selection"); idx != -1 {
		sub := content[idx:]
		if toIdx := strings.Index(sub, "to "); toIdx != -1 {
			modelSub := sub[toIdx+3:]
			if endIdx := strings.IndexAny(modelSub, "\n.)"); endIdx != -1 {
				return strings.TrimSpace(modelSub[:endIdx])
			}
			return strings.TrimSpace(modelSub)
		}
	}
	return ""
}

func subagentsEqual(a, b map[string]*Subagent) bool {
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

