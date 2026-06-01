package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jacobmiller22/hivemind/pkg/daemon"
)

var conversationIDRegex = regexp.MustCompile(`(?i)"conversationId"\s*:\s*"([a-fA-F0-9\-]+)"`)

type AntigravityAdapter struct {
	AntigravityDir   string
	FilePollInterval time.Duration
}

func NewAntigravityAdapter(dir string, interval time.Duration) *AntigravityAdapter {
	return &AntigravityAdapter{
		AntigravityDir:   dir,
		FilePollInterval: interval,
	}
}

func (a *AntigravityAdapter) Name() string {
	return "antigravity"
}

func (a *AntigravityAdapter) Start(ctx context.Context, s *daemon.Server) error {
	if a.AntigravityDir == "" {
		return nil
	}
	_ = os.MkdirAll(a.AntigravityDir, 0755)

	ticker := time.NewTicker(a.FilePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.SyncAntigravitySessions(s)
		}
	}
}

func (a *AntigravityAdapter) SyncAntigravitySessions(s *daemon.Server) {
	entries, err := os.ReadDir(a.AntigravityDir)
	if err != nil {
		return
	}

	now := time.Now()
	seenIDs := make(map[string]bool)
	stateChanged := false

	type parsedSession struct {
		key     string
		fss     daemon.FileSessionState
		modTime time.Time
	}

	var parsed []parsedSession
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		sessionID := entry.Name()
		logsDir := filepath.Join(a.AntigravityDir, sessionID, ".system_generated", "logs")
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

		fss, err := a.ParseTranscriptFile(transcriptPath, sessionID)
		if err != nil {
			continue
		}

		key := "file:antigravity:" + sessionID
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
					} else if sa.Status == daemon.SubagentCompleted || sa.Status == daemon.SubagentErrored {
						if sa.CompletedAt == nil {
							sa.CompletedAt = &modTime
						}
					}
				} else {
					sa.SpawnedAt = modTime
					if sa.Status == daemon.SubagentCompleted || sa.Status == daemon.SubagentErrored {
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
				!daemon.SubagentsEqual(session.Subagents, fss.Subagents) ||
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
				subagents = make(map[string]*daemon.Subagent)
			}
			// Align subagents for new session
			for _, sa := range subagents {
				sa.SpawnedAt = modTime
				if sa.Status == daemon.SubagentCompleted || sa.Status == daemon.SubagentErrored {
					sa.CompletedAt = &modTime
				}
			}
			s.State.Sessions[p.key] = &daemon.SessionState{
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
		if strings.HasPrefix(id, "file:antigravity:") {
			logsDir := filepath.Join(a.AntigravityDir, session.SessionID, ".system_generated", "logs")
			transcriptPath1 := filepath.Join(logsDir, "transcript.jsonl")
			transcriptPath2 := filepath.Join(logsDir, "transcript_full.jsonl")

			_, errTrans1 := os.Stat(transcriptPath1)
			_, errTrans2 := os.Stat(transcriptPath2)
			transExists := errTrans1 == nil || errTrans2 == nil

			if !transExists {
				delete(s.State.Sessions, id)
				stateChanged = true
			}
		}
	}

	if stateChanged {
		s.BroadcastState()
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

// ParseTranscriptFile reads a transcript JSONL file and reconstructs the session state
func (a *AntigravityAdapter) ParseTranscriptFile(path string, sessionID string) (*daemon.FileSessionState, error) {
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
	fss := &daemon.FileSessionState{
		SessionID:   sessionID,
		TmuxPaneID:  "%0",
		TmuxSession: "antigravity",
		TmuxWindow:  "1",
		Status:      daemon.StatusIdle,
		Subagents:   make(map[string]*daemon.Subagent),
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
					type SubagentArg struct {
						Role     string `json:"Role"`
						TypeName string `json:"TypeName"`
					}
					var sas []SubagentArg

					// 1. Try to unmarshal as direct struct first (raw JSON array)
					var args struct {
						Subagents []SubagentArg `json:"Subagents"`
					}
					if err := json.Unmarshal(tc.Args, &args); err == nil && len(args.Subagents) > 0 {
						sas = args.Subagents
					} else {
						// 2. Try to unmarshal tc.Args as a string representing the entire JSON object, or directly as an object
						var argsStr string
						var parsedObj map[string]json.RawMessage
						if json.Unmarshal(tc.Args, &argsStr) == nil {
							_ = json.Unmarshal([]byte(argsStr), &parsedObj)
						} else {
							_ = json.Unmarshal(tc.Args, &parsedObj)
						}

						if parsedObj != nil {
							if subagentsRaw, ok := parsedObj["Subagents"]; ok {
								// 3. Try to unmarshal the raw field as an array
								if err := json.Unmarshal(subagentsRaw, &sas); err != nil {
									// 4. Try to unmarshal the raw field as a string containing the array
									var subagentsStr string
									if json.Unmarshal(subagentsRaw, &subagentsStr) == nil {
										_ = json.Unmarshal([]byte(subagentsStr), &sas)
									}
								}
							}
						}
					}

					for idx, sa := range sas {
						saID := fmt.Sprintf("sub_%d_%d", line.StepIndex, idx)
						subagentsByStep[line.StepIndex] = append(subagentsByStep[line.StepIndex], saID)
						fss.Subagents[saID] = &daemon.Subagent{
							ID:        saID,
							Role:      sa.Role,
							TypeName:  sa.TypeName,
							Status:    daemon.SubagentRunning,
							SpawnedAt: time.Now(),
						}
					}
				}
			}
		}

		// Mark subagents as completed when the INVOKE_SUBAGENT tool execution returns
		if line.Type == "INVOKE_SUBAGENT" && line.Status == "DONE" {
			// Extract conversationId (child UUID) if present
			var childUUID string
			matches := conversationIDRegex.FindStringSubmatch(line.Content)
			if len(matches) > 1 {
				childUUID = matches[1]
			}

			// Find the temporary subagent ID that corresponds to this tool execution.
			// The tool call was at some step S < line.StepIndex. We look for the most recent
			// subagent with ID format "sub_S_idx".
			var targetTempID string
			maxStep := -1
			for saID := range fss.Subagents {
				if strings.HasPrefix(saID, "sub_") {
					var s, idx int
					if _, err := fmt.Sscanf(saID, "sub_%d_%d", &s, &idx); err == nil {
						if s < line.StepIndex && s > maxStep {
							maxStep = s
							targetTempID = saID
						}
					}
				}
			}

			if targetTempID != "" {
				sa := fss.Subagents[targetTempID]
				// If we extracted a real child UUID, we link/rename it!
				if childUUID != "" {
					delete(fss.Subagents, targetTempID)
					sa.ID = childUUID
					fss.Subagents[childUUID] = sa
				}
				
				// Mark as completed since the tool returned DONE
				sa.Status = daemon.SubagentCompleted
				completedAt := time.Now()
				sa.CompletedAt = &completedAt
			} else {
				// Fallback: if no temporary subagent was found, but we have a childUUID,
				// still ensure we have it registered as completed
				if childUUID != "" {
					if sa, exists := fss.Subagents[childUUID]; exists {
						sa.Status = daemon.SubagentCompleted
						completedAt := time.Now()
						sa.CompletedAt = &completedAt
					}
				}
			}
		}
	}

	// Apply status heuristic to the very last line
	lastLine := lines[len(lines)-1]
	switch lastLine.Type {
	case "USER_INPUT":
		fss.Status = daemon.StatusThinking
	case "PLANNER_RESPONSE":
		if lastLine.Status != "DONE" {
			fss.Status = daemon.StatusThinking
		} else {
			if len(lastLine.ToolCalls) > 0 {
				tc := lastLine.ToolCalls[0]
				switch tc.Name {
				case "ask_permission":
					fss.Status = daemon.StatusAwaitingPermission
				case "ask_question":
					fss.Status = daemon.StatusAwaitingInput
				default:
					fss.Status = daemon.StatusToolRunning
				}
			} else {
				fss.Status = daemon.StatusIdle
			}
		}
	default:
		// Tool execution steps
		if lastLine.Status != "DONE" {
			fss.Status = daemon.StatusToolRunning
		} else {
			fss.Status = daemon.StatusThinking // Tool just outputted; model about to plan/think
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
