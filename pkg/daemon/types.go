package daemon

import (
	"time"
)

// DerivedStatus represents the status of a developer session.
type DerivedStatus string

const (
	StatusIdle               DerivedStatus = "idle"
	StatusThinking           DerivedStatus = "thinking"
	StatusToolRunning        DerivedStatus = "tool-running"
	StatusAwaitingPermission DerivedStatus = "awaiting-permission"
	StatusAwaitingInput      DerivedStatus = "awaiting-input"
	StatusErrored            DerivedStatus = "errored"
	StatusNoTelemetry        DerivedStatus = "no-telemetry"
	StatusCompleted          DerivedStatus = "completed"
)

// SubagentStatus represents the status of a spawned subagent.
type SubagentStatus string

const (
	SubagentRunning   SubagentStatus = "running"
	SubagentCompleted SubagentStatus = "completed"
	SubagentErrored   SubagentStatus = "errored"
)

// Subagent represents the state of a spawned subagent.
type Subagent struct {
	ID          string         `json:"id"`
	Role        string         `json:"role"`
	TypeName    string         `json:"typeName"`
	Status      SubagentStatus `json:"status"`
	SpawnedAt   time.Time      `json:"spawnedAt"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"` // For cool-off window calculations
}

// EventContext contains the tmux and directory context in which the event was generated.
type EventContext struct {
	TmuxPaneId  string `json:"tmuxPaneId"`
	TmuxSession string `json:"tmuxSession"`
	TmuxWindow  string `json:"tmuxWindow"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch,omitempty"`
}

// SubagentPayload contains payload info for subagent events.
type SubagentPayload struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	TypeName string `json:"typeName"`
	Status   string `json:"status"` // 'running' | 'completed' | 'errored'
}

// EventPayload contains details specific to the event type.
type EventPayload struct {
	Status   DerivedStatus          `json:"status,omitempty"`
	Model    string                 `json:"model,omitempty"`
	Subagent *SubagentPayload       `json:"subagent,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// HivemindEvent is the standard event structure sent from adapters to the daemon.
type HivemindEvent struct {
	Type      string       `json:"type,omitempty"` // e.g. "event"
	EventID   string       `json:"eventId"`
	SessionID string       `json:"sessionId"`
	Timestamp time.Time    `json:"timestamp"`
	EventType string       `json:"eventType"` // 'session_started' | 'status_changed' | 'subagent_spawned' | 'subagent_status_changed' | 'session_stopped'
	Context   EventContext `json:"context"`
	Payload   EventPayload `json:"payload"`
}

// SessionState represents the aggregated in-memory state of a single parent developer session.
type SessionState struct {
	SessionID     string               `json:"sessionId"`
	TmuxPaneID    string               `json:"tmuxPaneId"`
	TmuxSession   string               `json:"tmuxSession"`
	TmuxWindow    string               `json:"tmuxWindow"`
	Cwd           string               `json:"cwd"`
	GitBranch     string               `json:"gitBranch,omitempty"`
	Model         string               `json:"model,omitempty"`
	Status        DerivedStatus        `json:"status"`
	LastEventTime time.Time            `json:"lastEventTimestamp"`
	Subagents     map[string]*Subagent `json:"subagents"` // subagent ID -> Subagent

	// Internal tracking fields for pruning & stale checks
	LastEventReceived time.Time  `json:"-"`
	PaneExited        bool       `json:"-"`
	PaneExitedAt      *time.Time `json:"-"`
}

// StateTree represents the entire aggregated state of all sessions.
type StateTree struct {
	Sessions map[string]*SessionState `json:"sessions"` // SessionID -> SessionState
}

// TmuxSessionState groups agent sessions by tmux session, matching client expectations.
type TmuxSessionState struct {
	Name     string                   `json:"name"`
	Sessions map[string]*SessionState `json:"sessions"`
}

// GroupedStateTree represents the hierarchical tree structure broadcasted to TUI clients.
type GroupedStateTree struct {
	TmuxSessions map[string]*TmuxSessionState `json:"tmuxSessions"`
}

// FileSessionState represents a session state read from a JSON file on disk.
type FileSessionState struct {
	SessionID   string               `json:"sessionId"`
	TmuxPaneID  string               `json:"tmuxPaneId"`
	TmuxSession string               `json:"tmuxSession"`
	TmuxWindow  string               `json:"tmuxWindow"`
	Cwd         string               `json:"cwd"`
	GitBranch   string               `json:"gitBranch,omitempty"`
	Model       string               `json:"model,omitempty"`
	Status      DerivedStatus        `json:"status"`
	Subagents   map[string]*Subagent `json:"subagents,omitempty"`
}
