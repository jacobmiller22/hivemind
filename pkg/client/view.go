package client

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Premium UI Colors
const (
	ColorBgDark      = "#121214"
	ColorBgCard      = "#1e1e24"
	ColorBorder      = "#4f46e5" // Royal Indigo
	ColorPurple      = "#a78bfa" // Neon Lavender
	ColorCyan        = "#06b6d4" // Electric Cyan
	ColorAmber       = "#f59e0b" // Warm Amber
	ColorEmerald     = "#10b981" // Soft Emerald
	ColorCrimson     = "#ef4444" // Vivid Crimson
	ColorSlate       = "#64748b" // Muted Slate
	ColorSelection   = "#2d2a45" // Deep Violet highlight background
	ColorDarkGray    = "#334155" // Subtler dark gray
	ColorBranchGuide = "#4338ca" // Dark Indigo for tree lines
)

// Lipgloss styles
var (
	// Layout containers
	docStyle = lipgloss.NewStyle().
			Padding(1, 2, 1, 2).
			Background(lipgloss.Color(ColorBgDark))

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(ColorCyan)).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ColorBorder)).
			MarginBottom(1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorSlate)).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color(ColorDarkGray)).
			PaddingTop(1).
			MarginTop(1)

	mainLayout = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ColorBorder)).
			Padding(1, 2).
			Background(lipgloss.Color(ColorBgCard))

	inspectorTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(ColorPurple)).
				MarginBottom(1)

	// Status Badges
	badgeBlocked = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color(ColorCrimson)).
			Foreground(lipgloss.Color("#ffffff")).
			Padding(0, 1).
			SetString("[!] BLOCKED")

	badgeThinking = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color(ColorBorder)).
			Foreground(lipgloss.Color("#ffffff")).
			Padding(0, 1).
			SetString("THINKING")

	badgeToolRunning = lipgloss.NewStyle().
				Bold(true).
				Background(lipgloss.Color("#ec4899")). // Vibrant pink
				Foreground(lipgloss.Color("#ffffff")).
				Padding(0, 1).
				SetString("RUNNING TOOL")

	badgeIdle = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color(ColorDarkGray)).
			Foreground(lipgloss.Color(ColorSlate)).
			Padding(0, 1).
			SetString("IDLE")

	badgeAwaitingInput = lipgloss.NewStyle().
				Bold(true).
				Background(lipgloss.Color(ColorAmber)).
				Foreground(lipgloss.Color(ColorBgDark)).
				Padding(0, 1).
				SetString("AWAITING INPUT")

	badgeErrored = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color(ColorCrimson)).
			Foreground(lipgloss.Color("#ffffff")).
			Padding(0, 1).
			SetString("ERRORED")

	badgeNoTelemetry = lipgloss.NewStyle().
				Bold(true).
				Background(lipgloss.Color("#1e293b")).
				Foreground(lipgloss.Color(ColorSlate)).
				Padding(0, 1).
				SetString("NO TELEMETRY")

	// Tree Node Styles
	selectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color(ColorSelection)).
				Bold(true)

	tmuxNodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorCyan)).
			Bold(true)

	agentNodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f8fafc")) // Bright white

	subagentNodeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorPurple))

	// Info Highlights
	gitBranchStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorAmber))

	modelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorEmerald))

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorSlate))
)

// View renders the Bubble Tea TUI
func (m *Model) View() string {
	// Fallback/boundary sizes
	width := m.Width
	if width == 0 {
		width = 85
	}
	height := m.Height
	if height == 0 {
		height = 24
	}

	// Calculate layout sizes
	bodyHeight := height - 9 // Space for header & footer
	if bodyHeight < 5 {
		bodyHeight = 5
	}

	var viewBuilder strings.Builder

	// 1. Render Header
	viewBuilder.WriteString(m.renderHeader(width))
	viewBuilder.WriteString("\n")

	// 2. Render Body (Tree on Left, Inspector Panel on Right if screen is wide enough)
	treeContent := m.renderTree(bodyHeight)
	
	var body string
	if width >= 90 {
		// Side-by-side layout
		inspectorWidth := 34
		treeWidth := width - inspectorWidth - 6
		
		inspectorContent := m.renderInspector(inspectorWidth, bodyHeight)
		
		body = lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(treeWidth).Render(treeContent),
			lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(lipgloss.Color(ColorDarkGray)).
				PaddingLeft(2).
				Width(inspectorWidth).
				Render(inspectorContent),
		)
	} else {
		// Stacked or tree-only layout
		body = treeContent
	}

	viewBuilder.WriteString(mainLayout.Width(width - 6).Render(body))
	viewBuilder.WriteString("\n")

	// 3. Render Footer
	viewBuilder.WriteString(m.renderFooter(width))

	return docStyle.Render(viewBuilder.String())
}

// renderHeader renders the top dashboard title and global states
func (m *Model) renderHeader(width int) string {
	var statusIndicator string
	var statusColor string
	
	if m.DemoMode {
		statusIndicator = "● DEMO (OFFLINE)"
		statusColor = ColorAmber
	} else if m.Connected {
		statusIndicator = "● LIVE DAEMON"
		statusColor = ColorEmerald
	} else {
		statusIndicator = "○ DISCONNECTED"
		statusColor = ColorCrimson
	}

	liveBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color(statusColor)).
		Bold(true).
		Render(statusIndicator)

	// Count statistics
	totalSessions := 0
	blockedSessions := 0
	runningAgents := 0
	subagentsActive := 0

	for _, tSession := range m.State.TmuxSessions {
		for _, session := range tSession.Sessions {
			totalSessions++
			if session.Status == "awaiting-permission" {
				blockedSessions++
			} else if session.Status == "thinking" || session.Status == "tool-running" {
				runningAgents++
			}
			for _, sa := range session.Subagents {
				if sa.Status == "running" {
					subagentsActive++
				}
			}
		}
	}

	statsText := fmt.Sprintf("Parent Sessions: %d | Running: %d | Blocked: %d | Subagents: %d", 
		totalSessions, runningAgents, blockedSessions, subagentsActive)
	
	headerContent := fmt.Sprintf(" H I V E M I N D   D A S H B O A R D   %s\n %s", 
		strings.Repeat(" ", int(math.Max(4, float64(width-len("HIVEMIND DASHBOARD")-len(statusIndicator)-15)))), liveBadge)
	
	statsBar := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorSlate)).
		Width(width - 10).
		Render(statsText)

	return headerStyle.Width(width - 6).Render(headerContent + "\n " + statsBar)
}

// renderTree renders the tree hierarchy
func (m *Model) renderTree(maxLines int) string {
	if len(m.FlattenedNodes) == 0 {
		return mutedStyle.Render("No active agent sessions discovered.\nEnsure your Antigravity agent sessions are running and hooks are installed.")
	}

	var lines []string
	
	// Viewport logic: scroll window around selected index
	start := 0
	end := len(m.FlattenedNodes)
	
	if end > maxLines {
		half := maxLines / 2
		start = m.SelectedIndex - half
		if start < 0 {
			start = 0
		}
		end = start + maxLines
		if end > len(m.FlattenedNodes) {
			end = len(m.FlattenedNodes)
			start = end - maxLines
		}
	}

	for i := start; i < end; i++ {
		node := m.FlattenedNodes[i]
		isSelected := (i == m.SelectedIndex)
		
		var line string
		switch node.Type {
		case NodeTypeTmuxSession:
			line = m.renderTmuxSessionNode(node)
		case NodeTypeAgentSession:
			line = m.renderAgentSessionNode(node)
		case NodeTypeSubagent:
			line = m.renderSubagentNode(node)
		}

		if isSelected {
			// Prepend selection pointer
			pointer := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorPurple)).Bold(true).Render("❯ ")
			line = selectedRowStyle.Render(pointer + line)
		} else {
			line = "  " + line
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// renderTmuxSessionNode renders a top-level tmux session row
func (m *Model) renderTmuxSessionNode(node *FlattenedNode) string {
	arrow := "▸ "
	if node.Expanded {
		arrow = "▾ "
	}
	
	folderIcon := "󰚗 " // Tmux session symbol / host
	label := tmuxNodeStyle.Render(node.Label)
	countText := mutedStyle.Render(fmt.Sprintf(" (%d active agents)", len(m.State.TmuxSessions[node.Label].Sessions)))

	return arrow + folderIcon + label + countText
}

// renderAgentSessionNode renders a parent agent session row
func (m *Model) renderAgentSessionNode(node *FlattenedNode) string {
	s := node.AgentSession
	if s == nil {
		return node.Label
	}

	// Dynamic Guide characters
	guideColor := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBranchGuide))
	branchGuide := guideColor.Render("├── ")
	
	// Let's see if this is the last agent in the tmux session (this is a nice detail!)
	parentSession := m.State.TmuxSessions[node.TmuxSessionName]
	if parentSession != nil {
		lastSessionID := ""
		// Sort keys to match rebuildTree
		var keys []string
		for k := range parentSession.Sessions {
			keys = append(keys, k)
		}
		if len(keys) > 0 {
			lastSessionID = keys[len(keys)-1] // very simple check, let's keep it robust
		}
		if s.SessionID == lastSessionID && !node.Expanded {
			branchGuide = guideColor.Render("└── ")
		}
	}

	arrow := "▸ "
	if node.Expanded {
		arrow = "▾ "
	}

	// Format text elements
	tmuxCoords := fmt.Sprintf("[%s:%s.%s]", s.TmuxSession, s.TmuxWindow, s.TmuxPaneID)
	tmuxCoords = mutedStyle.Render(tmuxCoords)
	
	// Short CWD
	shortCwd := filepath.Base(s.Cwd)
	if shortCwd == "." || shortCwd == "/" {
		shortCwd = s.Cwd
	}
	cwdText := fmt.Sprintf(" %s/", shortCwd)
	
	branchText := ""
	if s.GitBranch != "" {
		branchText = gitBranchStyle.Render("  " + s.GitBranch)
	}

	// Status Badge
	badge := m.getStatusBadge(s.Status)

	return branchGuide + arrow + tmuxCoords + cwdText + branchText + "  " + badge
}

// renderSubagentNode renders a child subagent session row
func (m *Model) renderSubagentNode(node *FlattenedNode) string {
	sa := node.Subagent
	if sa == nil {
		return node.Label
	}

	guideColor := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBranchGuide))
	// Deep guides
	guides := guideColor.Render("│   └── ")

	// Render status indicator
	statusColor := ColorSlate
	switch sa.Status {
	case "running":
		statusColor = ColorPurple
	case "completed":
		statusColor = ColorEmerald
	case "errored":
		statusColor = ColorCrimson
	}

	statusIndicator := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Bold(true).Render("● " + sa.Status)

	// Calculate elapsed time
	elapsed := time.Since(sa.SpawnedAt)
	elapsedText := mutedStyle.Render(fmt.Sprintf("(%s)", formatDuration(elapsed)))

	roleLabel := subagentNodeStyle.Render(sa.Role)
	typeText := mutedStyle.Render(" [" + sa.TypeName + "]")

	return guides + roleLabel + typeText + "  " + statusIndicator + "  " + elapsedText
}

// renderInspector renders the side detail inspector panel
func (m *Model) renderInspector(width int, height int) string {
	if m.SelectedIndex < 0 || m.SelectedIndex >= len(m.FlattenedNodes) {
		return ""
	}
	
	node := m.FlattenedNodes[m.SelectedIndex]
	var content []string

	content = append(content, inspectorTitleStyle.Render("DETAILS INSPECTOR"))

	switch node.Type {
	case NodeTypeTmuxSession:
		content = append(content, fmt.Sprintf("Type:   %s", tmuxNodeStyle.Render("Tmux Session")))
		content = append(content, fmt.Sprintf("Name:   %s", node.Label))
		
		sessions := m.State.TmuxSessions[node.Label]
		if sessions != nil {
			content = append(content, fmt.Sprintf("Agents: %d registered", len(sessions.Sessions)))
		}

	case NodeTypeAgentSession:
		s := node.AgentSession
		if s == nil {
			break
		}
		content = append(content, fmt.Sprintf("Type:     %s", agentNodeStyle.Render("Parent Agent")))
		content = append(content, fmt.Sprintf("Session:  %s", s.SessionID))
		content = append(content, fmt.Sprintf("Tmux:     %s:%s pane %s", s.TmuxSession, s.TmuxWindow, s.TmuxPaneID))
		content = append(content, fmt.Sprintf("Model:    %s", modelStyle.Render(s.Model)))
		content = append(content, fmt.Sprintf("CWD:\n  %s", mutedStyle.Render(s.Cwd)))
		if s.GitBranch != "" {
			content = append(content, fmt.Sprintf("Branch:   %s", gitBranchStyle.Render(s.GitBranch)))
		}
		content = append(content, fmt.Sprintf("Status:   %s", m.getStatusBadge(s.Status)))
		
		lastSeen := time.Since(s.LastEventTimestamp)
		content = append(content, fmt.Sprintf("Last Act: %s ago", formatDuration(lastSeen)))
		content = append(content, fmt.Sprintf("Children: %d subagents", len(s.Subagents)))

	case NodeTypeSubagent:
		sa := node.Subagent
		if sa == nil {
			break
		}
		content = append(content, fmt.Sprintf("Type:    %s", subagentNodeStyle.Render("Sub-Agent")))
		content = append(content, fmt.Sprintf("Role:    %s", sa.Role))
		content = append(content, fmt.Sprintf("Driver:  %s", sa.TypeName))
		content = append(content, fmt.Sprintf("Status:  %s", sa.Status))
		
		elapsed := time.Since(sa.SpawnedAt)
		content = append(content, fmt.Sprintf("Elapsed: %s", formatDuration(elapsed)))
		content = append(content, fmt.Sprintf("Spawned: %s ago", formatDuration(elapsed)))
		
		if node.Parent != nil && node.Parent.AgentSession != nil {
			p := node.Parent.AgentSession
			content = append(content, fmt.Sprintf("Parent:  %s", p.TmuxPaneID))
		}
	}

	// Fit panel to height
	lines := strings.Split(strings.Join(content, "\n\n"), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	
	return strings.Join(lines, "\n")
}

// renderFooter renders instructions and active status notifications
func (m *Model) renderFooter(width int) string {
	helpText := "↑/↓, j/k: Nav • Space/Enter: Expand • g: tmux-Jump • r: Refresh • q: Quit"
	
	status := m.statusMessage
	if status == "" {
		if m.SelectedIndex < len(m.FlattenedNodes) && m.SelectedIndex >= 0 {
			node := m.FlattenedNodes[m.SelectedIndex]
			if node.Type == NodeTypeAgentSession && node.AgentSession != nil {
				status = fmt.Sprintf("Press 'g' to switch active terminal to %s:%s.%s", 
					node.AgentSession.TmuxSession, node.AgentSession.TmuxWindow, node.AgentSession.TmuxPaneID)
			}
		}
	}
	
	if status != "" {
		// Highlight footer status in purple/emerald
		status = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorPurple)).Bold(true).Render("ℹ " + status)
	} else {
		status = mutedStyle.Render("Idle dashboard")
	}

	helpBar := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorSlate)).
		Render(helpText)

	return footerStyle.Width(width - 6).Render(status + "\n" + helpBar)
}

// getStatusBadge returns the custom styled lipgloss badge for an agent status
func (m *Model) getStatusBadge(status string) string {
	switch status {
	case "awaiting-permission":
		// Custom Glowing Amber/Red badge that stands out clearly [!] BLOCKED
		return badgeBlocked.Render()
	case "thinking":
		return badgeThinking.Render()
	case "tool-running":
		return badgeToolRunning.Render()
	case "idle":
		return badgeIdle.Render()
	case "awaiting-input":
		return badgeAwaitingInput.Render()
	case "errored":
		return badgeErrored.Render()
	case "no-telemetry":
		return badgeNoTelemetry.Render()
	default:
		return lipgloss.NewStyle().Background(lipgloss.Color(ColorSlate)).Foreground(lipgloss.Color("#ffffff")).Padding(0, 1).Render(strings.ToUpper(status))
	}
}

// formatDuration format durations into human readable strings like "2m 15s" or "3s"
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	
	secs := int(d.Seconds())
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	
	mins := secs / 60
	remSecs := secs % 60
	
	if mins < 60 {
		return fmt.Sprintf("%dm %ds", mins, remSecs)
	}
	
	hours := mins / 60
	remMins := mins % 60
	
	return fmt.Sprintf("%dh %dm", hours, remMins)
}
