package adapters

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jacobmiller22/hivemind/pkg/daemon"
)

type MockFileAdapter struct {
	SessionsDir      string
	FilePollInterval time.Duration
}

func NewMockFileAdapter(sessionsDir string, interval time.Duration) *MockFileAdapter {
	return &MockFileAdapter{
		SessionsDir:      sessionsDir,
		FilePollInterval: interval,
	}
}

func (a *MockFileAdapter) Name() string {
	return "mock-file"
}

func (a *MockFileAdapter) Start(ctx context.Context, s *daemon.Server) error {
	_ = os.MkdirAll(a.SessionsDir, 0755)

	ticker := time.NewTicker(a.FilePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.SyncFileSessions(s)
		}
	}
}

func (a *MockFileAdapter) SyncFileSessions(s *daemon.Server) {
	entries, err := os.ReadDir(a.SessionsDir)
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
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(a.SessionsDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var fss daemon.FileSessionState
		if err := json.Unmarshal(data, &fss); err != nil {
			continue
		}

		fileInfo, err := entry.Info()
		modTime := now
		if err == nil {
			modTime = fileInfo.ModTime()
		}

		key := "file:mock:" + fss.SessionID
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

	// Prune file-based sessions whose JSON file was deleted
	for id, session := range s.State.Sessions {
		if strings.HasPrefix(id, "file:mock:") {
			jsonPath := filepath.Join(a.SessionsDir, session.SessionID+".json")
			_, errJson := os.Stat(jsonPath)
			if errJson != nil {
				delete(s.State.Sessions, id)
				stateChanged = true
			}
		}
	}

	if stateChanged {
		s.BroadcastState()
	}
}
