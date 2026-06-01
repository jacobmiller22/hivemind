package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// NodeType represents the type of tree node
type NodeType int

const (
	NodeTypeTmuxSession NodeType = iota
	NodeTypeAgentSession
	NodeTypeSubagent
)

// SubagentState represents a subagent's telemetry state
type SubagentState struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	TypeName  string    `json:"typeName"`
	Status    string    `json:"status"` // "running", "completed", "errored"
	SpawnedAt time.Time `json:"spawnedAt"`
}

// AgentSessionState represents a parent agent session's telemetry state
type AgentSessionState struct {
	SessionID          string                    `json:"sessionId"`
	TmuxPaneID         string                    `json:"tmuxPaneId"`
	TmuxSession        string                    `json:"tmuxSession"`
	TmuxWindow         string                    `json:"tmuxWindow"`
	Cwd                string                    `json:"cwd"`
	GitBranch          string                    `json:"gitBranch,omitempty"`
	Model              string                    `json:"model,omitempty"`
	Status             string                    `json:"status"` // derived status e.g. "idle", "thinking", "awaiting-permission", etc.
	LastEventTimestamp time.Time                 `json:"lastEventTimestamp"`
	Subagents          map[string]*SubagentState `json:"subagents,omitempty"`
}

// TmuxSessionState groups agent sessions by tmux session
type TmuxSessionState struct {
	Name     string                        `json:"name"`
	Sessions map[string]*AgentSessionState `json:"sessions"`
}

// StateTree represents the full aggregated state tree broadcasted by the daemon
type StateTree struct {
	TmuxSessions map[string]*TmuxSessionState `json:"tmuxSessions"`
}

// FlattenedNode represents a single visible row in our tree view
type FlattenedNode struct {
	ID              string
	Type            NodeType
	Depth           int
	Expanded        bool
	HasChildren     bool
	Label           string
	TmuxSessionName string
	AgentSession    *AgentSessionState
	Subagent        *SubagentState
	Parent          *FlattenedNode
}

// Model is the main Bubble Tea model
type Model struct {
	State          StateTree
	Connected      bool
	UdsPath        string
	SelectedIndex  int
	FlattenedNodes []*FlattenedNode
	ExpandedNodes  map[string]bool // ID -> expanded status
	Width          int
	Height         int
	LastUpdate     time.Time
	Error          error
	
	// Mode configuration
	DemoMode       bool
	udsChan        chan tea.Msg
	ctx            context.Context
	cancel         context.CancelFunc
	mutex          sync.Mutex
	statusMessage  string
	statusTimer    *time.Timer
}

// Message types for Bubble Tea
type udsConnectMsg struct{}
type udsDisconnectMsg struct{ err error }
type udsStateMsg struct{ state StateTree }
type statusMsg string
type tickMsg time.Time

func NewModel(udsPath string, demoMode bool) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	return &Model{
		State: StateTree{
			TmuxSessions: make(map[string]*TmuxSessionState),
		},
		Connected:     false,
		UdsPath:       udsPath,
		SelectedIndex: 0,
		ExpandedNodes: map[string]bool{
			// Pre-expand sessions by default for a nice welcoming tree
			"demo_tmux_hivemind": true,
			"demo_tmux_web-app":  true,
		},
		DemoMode:      demoMode,
		udsChan:       make(chan tea.Msg, 10),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Init initializes the Bubble Tea program
func (m *Model) Init() tea.Cmd {
	if m.DemoMode {
		m.loadDemoData()
		m.rebuildTree()
		return tea.Batch(
			m.showStatus("Running in DEMO Mode (Offline Mock)", 4*time.Second),
			tickCmd(),
		)
	}

	// Start background UDS listener
	go m.listenToUDS()
	
	return tea.Batch(
		m.waitForUDS(),
		m.showStatus("Connecting to daemon UDS...", 3*time.Second),
		tickCmd(),
	)
}

// Update handles state transitions and keyboard input
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.cancel()
			return m, tea.Quit

		case "up", "k":
			if m.SelectedIndex > 0 {
				m.SelectedIndex--
			}
			return m, nil

		case "down", "j":
			if m.SelectedIndex < len(m.FlattenedNodes)-1 {
				m.SelectedIndex++
			}
			return m, nil

		case "space", "enter":
			m.toggleSelectedNode()
			return m, nil

		case "g":
			// Jump to TMUX pane
			cmd := m.jumpToSelectedTmuxPane()
			return m, cmd

		case "r":
			if !m.DemoMode {
				// Reconnect attempt
				m.cancel()
				m.ctx, m.cancel = context.WithCancel(context.Background())
				go m.listenToUDS()
				return m, tea.Batch(
					m.waitForUDS(),
					m.showStatus("Reconnecting to daemon...", 2*time.Second),
				)
			} else {
				m.loadDemoData()
				m.rebuildTree()
				return m, m.showStatus("Reloaded demo mock state", 2*time.Second)
			}
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case udsConnectMsg:
		m.Connected = true
		m.Error = nil
		return m, tea.Batch(
			m.waitForUDS(),
			m.showStatus("Connected to hivemind daemon", 3*time.Second),
		)

	case udsDisconnectMsg:
		m.Connected = false
		m.Error = msg.err
		m.rebuildTree()
		return m, tea.Batch(
			m.waitForUDS(),
			m.showStatus(fmt.Sprintf("Disconnected: %v", msg.err), 5*time.Second),
		)

	case udsStateMsg:
		m.State = msg.state
		m.Connected = true
		m.LastUpdate = time.Now()
		m.rebuildTree()
		return m, m.waitForUDS()

	case statusMsg:
		m.statusMessage = string(msg)
		return m, nil

	case tickMsg:
		// Periodic tick to update subagent elapsed times
		if m.DemoMode {
			// Simulate some updates in demo mode to make the TUI look alive!
			m.animateDemoData()
			m.rebuildTree()
		}
		return m, tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}

	return m, nil
}

// toggleSelectedNode expands or collapses the currently selected node
func (m *Model) toggleSelectedNode() {
	if m.SelectedIndex < 0 || m.SelectedIndex >= len(m.FlattenedNodes) {
		return
	}
	node := m.FlattenedNodes[m.SelectedIndex]
	if !node.HasChildren {
		return
	}
	
	current := m.ExpandedNodes[node.ID]
	m.ExpandedNodes[node.ID] = !current
	m.rebuildTree()
}

// jumpToSelectedTmuxPane switches tmux to the selected pane
func (m *Model) jumpToSelectedTmuxPane() tea.Cmd {
	if m.SelectedIndex < 0 || m.SelectedIndex >= len(m.FlattenedNodes) {
		return nil
	}
	node := m.FlattenedNodes[m.SelectedIndex]
	var paneID string
	var label string

	if node.Type == NodeTypeAgentSession && node.AgentSession != nil {
		paneID = node.AgentSession.TmuxPaneID
		label = fmt.Sprintf("%s:%s.%s", node.AgentSession.TmuxSession, node.AgentSession.TmuxWindow, node.AgentSession.TmuxPaneID)
	} else if node.Type == NodeTypeSubagent && node.Subagent != nil {
		// Subagents don't always have a distinct tmux pane, but if the parent does, we can jump to it
		if node.Parent != nil && node.Parent.AgentSession != nil {
			paneID = node.Parent.AgentSession.TmuxPaneID
			label = fmt.Sprintf("Parent %s:%s.%s", node.Parent.AgentSession.TmuxSession, node.Parent.AgentSession.TmuxWindow, node.Parent.AgentSession.TmuxPaneID)
		}
	}

	if paneID == "" {
		return m.showStatus("No tmux pane target for selection", 2*time.Second)
	}

	// Run tmux command to jump
	return func() tea.Msg {
		// Try select-pane -t paneID first
		cmd := exec.Command("tmux", "select-pane", "-t", paneID)
		_ = cmd.Run()
		
		// Also run select-window -t paneID just in case select-pane doesn't trigger window switch in all tmux setups
		cmd2 := exec.Command("tmux", "select-window", "-t", paneID)
		_ = cmd2.Run()

		return statusMsg(fmt.Sprintf("Jumped to tmux pane %s", label))
	}
}

// rebuildTree converts our hierarchical StateTree into a flat list of visible nodes
func (m *Model) rebuildTree() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var newNodes []*FlattenedNode

	// Sort tmux sessions by name for stable ordering
	var tmuxNames []string
	for k := range m.State.TmuxSessions {
		tmuxNames = append(tmuxNames, k)
	}
	sort.Strings(tmuxNames)

	for _, tName := range tmuxNames {
		tSession := m.State.TmuxSessions[tName]
		tNodeID := "tmux_" + tName
		tExpanded := m.ExpandedNodes[tNodeID]
		hasAgents := len(tSession.Sessions) > 0

		tmuxNode := &FlattenedNode{
			ID:              tNodeID,
			Type:            NodeTypeTmuxSession,
			Depth:           0,
			Expanded:        tExpanded,
			HasChildren:     hasAgents,
			Label:           tName,
			TmuxSessionName: tName,
		}
		newNodes = append(newNodes, tmuxNode)

		if tExpanded && hasAgents {
			// Sort parent sessions by window/pane indices
			var sessionKeys []string
			for k := range tSession.Sessions {
				sessionKeys = append(sessionKeys, k)
			}
			sort.Slice(sessionKeys, func(i, j int) bool {
				s1 := tSession.Sessions[sessionKeys[i]]
				s2 := tSession.Sessions[sessionKeys[j]]
				if s1.TmuxWindow != s2.TmuxWindow {
					return s1.TmuxWindow < s2.TmuxWindow
				}
				return s1.TmuxPaneID < s2.TmuxPaneID
			})

			for _, sKey := range sessionKeys {
				session := tSession.Sessions[sKey]
				sNodeID := "session_" + session.SessionID
				sExpanded := m.ExpandedNodes[sNodeID]
				hasSubagents := len(session.Subagents) > 0

				sessionNode := &FlattenedNode{
					ID:              sNodeID,
					Type:            NodeTypeAgentSession,
					Depth:           1,
					Expanded:        sExpanded,
					HasChildren:     hasSubagents,
					Label:           fmt.Sprintf("Pane %s (%s)", session.TmuxPaneID, session.Model),
					TmuxSessionName: tName,
					AgentSession:    session,
				}
				newNodes = append(newNodes, sessionNode)

				if sExpanded && hasSubagents {
					// Sort subagents by spawned timestamp
					var subagentIDs []string
					for k := range session.Subagents {
						subagentIDs = append(subagentIDs, k)
					}
					sort.Slice(subagentIDs, func(i, j int) bool {
						sa1 := session.Subagents[subagentIDs[i]]
						sa2 := session.Subagents[subagentIDs[j]]
						return sa1.SpawnedAt.Before(sa2.SpawnedAt)
					})

					for _, saID := range subagentIDs {
						sa := session.Subagents[saID]
						saNodeID := "subagent_" + session.SessionID + "_" + sa.ID
						
						subagentNode := &FlattenedNode{
							ID:              saNodeID,
							Type:            NodeTypeSubagent,
							Depth:           2,
							Expanded:        false,
							HasChildren:     false,
							Label:           sa.Role,
							TmuxSessionName: tName,
							AgentSession:    session,
							Subagent:        sa,
						}
						// Track parent so subagents can find parent pane coordinates
						subagentNode.Parent = sessionNode
						newNodes = append(newNodes, subagentNode)
					}
				}
			}
		}
	}

	m.FlattenedNodes = newNodes
	
	// Bounds check selected index
	if m.SelectedIndex >= len(m.FlattenedNodes) {
		m.SelectedIndex = len(m.FlattenedNodes) - 1
	}
	if m.SelectedIndex < 0 {
		m.SelectedIndex = 0
	}
}

// listenToUDS connects to UDS and feeds messages to the channel
func (m *Model) listenToUDS() {
	var conn net.Conn
	var err error

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
			// Connect to socket
			conn, err = net.Dial("unix", m.UdsPath)
			if err != nil {
				// Try fallback if primary fails
				fallbackPath := "/tmp/hivemind.sock"
				if m.UdsPath != fallbackPath {
					conn, err = net.Dial("unix", fallbackPath)
				}
			}

			if err != nil {
				m.udsChan <- udsDisconnectMsg{err: err}
				time.Sleep(2 * time.Second)
				continue
			}

			// Send subscribe message to daemon so it registers this client and streams state updates
			_, err = conn.Write([]byte("{\"type\":\"subscribe\"}\n"))
			if err != nil {
				conn.Close()
				m.udsChan <- udsDisconnectMsg{err: err}
				time.Sleep(2 * time.Second)
				continue
			}

			m.udsChan <- udsConnectMsg{}

			reader := bufio.NewReader(conn)
			for {
				line, err := reader.ReadBytes('\n')
				if err != nil {
					conn.Close()
					m.udsChan <- udsDisconnectMsg{err: err}
					break
				}

				var state StateTree
				if err := json.Unmarshal(line, &state); err != nil {
					continue
				}

				m.udsChan <- udsStateMsg{state: state}
			}
		}
	}
}

// waitForUDS waits for a message from the background socket goroutine
func (m *Model) waitForUDS() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-m.udsChan:
			return msg
		case <-m.ctx.Done():
			return nil
		}
	}
}

// showStatus displays a status message in the footer for a set duration
func (m *Model) showStatus(message string, duration time.Duration) tea.Cmd {
	if m.statusTimer != nil {
		m.statusTimer.Stop()
	}
	m.statusTimer = time.NewTimer(duration)
	
	return tea.Batch(
		func() tea.Msg { return statusMsg(message) },
		func() tea.Msg {
			<-m.statusTimer.C
			return statusMsg("")
		},
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// --- MOCK / DEMO DATA INITIALIZATION ---

func (m *Model) loadDemoData() {
	now := time.Now()
	
	m.State = StateTree{
		TmuxSessions: map[string]*TmuxSessionState{
			"hivemind": {
				Name: "hivemind",
				Sessions: map[string]*AgentSessionState{
					"session_parent_01": {
						SessionID:          "session_parent_01",
						TmuxPaneID:         "%1",
						TmuxSession:        "hivemind",
						TmuxWindow:         "1",
						Cwd:                "/Users/jacobmiller22/projects/hivemind",
						GitBranch:          "feature/tui-client",
						Model:              "Gemini 1.5 Pro",
						Status:             "awaiting-permission",
						LastEventTimestamp: now.Add(-45 * time.Second),
						Subagents: map[string]*SubagentState{
							"sub_sa_01": {
								ID:        "sub_sa_01",
								Role:      "Codebase Researcher",
								TypeName:  "self",
								Status:    "running",
								SpawnedAt: now.Add(-5 * time.Minute),
							},
							"sub_sa_02": {
								ID:        "sub_sa_02",
								Role:      "Database Debugger",
								TypeName:  "db_helper",
								Status:    "completed",
								SpawnedAt: now.Add(-12 * time.Minute),
							},
						},
					},
					"session_parent_02": {
						SessionID:          "session_parent_02",
						TmuxPaneID:         "%3",
						TmuxSession:        "hivemind",
						TmuxWindow:         "2",
						Cwd:                "/Users/jacobmiller22/projects/hivemind/src/hooks",
						GitBranch:          "feature/tui-client",
						Model:              "Gemini 1.5 Flash",
						Status:             "thinking",
						LastEventTimestamp: now.Add(-5 * time.Second),
						Subagents:          make(map[string]*SubagentState),
					},
				},
			},
			"web-app": {
				Name: "web-app",
				Sessions: map[string]*AgentSessionState{
					"session_parent_03": {
						SessionID:          "session_parent_03",
						TmuxPaneID:         "%12",
						TmuxSession:        "web-app",
						TmuxWindow:         "1",
						Cwd:                "/Users/jacobmiller22/projects/nextjs-dashboard",
						GitBranch:          "main",
						Model:              "Gemini 1.5 Pro",
						Status:             "tool-running",
						LastEventTimestamp: now.Add(-12 * time.Second),
						Subagents: map[string]*SubagentState{
							"sub_sa_03": {
								ID:        "sub_sa_03",
								Role:      "CSS Styling Specialist",
								TypeName:  "designer",
								Status:    "running",
								SpawnedAt: now.Add(-2 * time.Minute),
							},
						},
					},
					"session_parent_04": {
						SessionID:          "session_parent_04",
						TmuxPaneID:         "%15",
						TmuxSession:        "web-app",
						TmuxWindow:         "2",
						Cwd:                "/Users/jacobmiller22/projects/nextjs-dashboard/api",
						GitBranch:          "main",
						Model:              "Gemini 1.5 Flash",
						Status:             "idle",
						LastEventTimestamp: now.Add(-3 * time.Minute),
						Subagents:          make(map[string]*SubagentState),
					},
					"session_parent_05": {
						SessionID:          "session_parent_05",
						TmuxPaneID:         "%18",
						TmuxSession:        "web-app",
						TmuxWindow:         "3",
						Cwd:                "/Users/jacobmiller22/projects/nextjs-dashboard/tests",
						GitBranch:          "main",
						Model:              "Claude 3.5 Sonnet",
						Status:             "no-telemetry",
						LastEventTimestamp: now.Add(-10 * time.Minute),
						Subagents:          make(map[string]*SubagentState),
					},
				},
			},
		},
	}
	m.Connected = true
	m.LastUpdate = now
}

// animateDemoData makes statuses shift occasionally so the interface feels dynamic and alive!
func (m *Model) animateDemoData() {
	// Periodic random fluctuations
	now := time.Now()
	m.LastUpdate = now

	// 1. Shift session 2 between "thinking", "tool-running", and "idle"
	s2 := m.State.TmuxSessions["hivemind"].Sessions["session_parent_02"]
	if s2 != nil {
		s2.LastEventTimestamp = now
		switch s2.Status {
		case "thinking":
			s2.Status = "tool-running"
		case "tool-running":
			s2.Status = "idle"
		default:
			s2.Status = "thinking"
		}
	}

	// 2. Add an elapsed second indicator or toggle a subagent status
	s3 := m.State.TmuxSessions["web-app"].Sessions["session_parent_03"]
	if s3 != nil {
		sub := s3.Subagents["sub_sa_03"]
		if sub != nil && now.Second()%20 == 0 {
			if sub.Status == "running" {
				sub.Status = "completed"
			} else {
				sub.Status = "running"
				sub.SpawnedAt = now
			}
		}
	}
}

// Helper: resolve user home directory in path
func ResolvePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
