# `hivemind` - Product Requirements Document

## 1. Overview

**hivemind** is a tmux-native TUI dashboard for monitoring Claude Code sessions and the sub-agents they spawn. It runs as a long-lived pane (typically the first pane of a tmux session) and provides a single vantage point onto every active agent across the machine.

### Problem

A productive Claude Code workflow involves many concurrent sessions - multiple tmux sessions per project, multiple panes per session, each running its own Claude Code instance with its own delegated sub-agents (tasks, debriefing, etc.). Today there is no way to see, at a glance:

- Which sessions are working vs. idle vs. stuck.
- Which sessions are blocked awaiting a permission prompt (acutely painful in sandboxed corporate environments where prompts are frequent).
- What sub-agents are running underneath each parent session, and which are still in flight.

The user must manually tab through panes to check each session, which scales poorly past 3-4 concurrent agents and erodes the parallelism benefit that delegation is supposed to provide.

### Goals (MVP1)

- **Visibility-first.** Provide a global, real-time view of every Claude Code session and its sub-agents, with a derived status for each.
- **Pull up hivemind from anywhere, pick up where you left off.** Launching the dashboard in any tmux pane shows the same live, shared state. Multiple concurrent dashboards stay in sync. Closing and reopening a dashboard never loses context - sessions, sub-agents, and their statuses persist independently of any particular client.
- **Pluggable telemetry sources.** Use Claude Code hooks as the primary event source for MVP1, but design so alternative sources (transcript watching, output scraping, hooks from other tools like coder or aider) can be added later without changing the dashboard experience.
- **Forward-compatible with MVP2 control.** Support the MVP2 use case of intercepting and remotely approving/denying permission prompts from any active dashboard.

### Non-goals (MVP1)

- Interactive control of sessions (kill, send input, approve prompts) - deferred to MVP2.
- Historical/audit views, cost/token telemetry - deferred.
- Support for non-Claude-Code agents - architected for, but not shipped.
- Cross-machine / network-accessible dashboards - MVP1 is single-machine only.

---

## 2. Personas & User Stories

### Primary persona: the parallel operator

A software engineer who runs multiple Claude Code sessions concurrently across several projects. Typical day:

- 2-4 active tmux sessions, one per project they're touching.
- 1-5 Claude Code panes per tmux session, each working a different slice of work (feature branch, bug investigation, doc-writing, exploratory research).
- Heavy use of sub-agent delegation - most parent sessions spawn child/delegated tasks for focused edits that block forward progress until acknowledged.
- Operates in a sandboxed corporate environment where Claude Code surfaces frequent permission prompts that block the parent until acknowledged.

The parallel operator's core frustration: **The cost of maintaining situational awareness scales worse than linearly with the number of agents.** Every additional agent adds context overhead, missed prompts, and forgotten sessions.

### User stories (MVP1)

**US-1 - Glance at the swarm.**
**As the operator, I want to open hivemind in any tmux pane and immediately see every active Claude Code session on my machine, grouped by tmux session, so I can take stock of what's running without tabbing through panes.**

**US-2 - Spot what needs attention.**
**As the operator, I want each session to show a clear status (idle / thinking / tool-running / awaiting-permission / awaiting-input / errored) so I can instantly see which sessions are blocked or in an error state and intervene.**

**US-3 - See sub-agent fan-out.**
**As the operator, I want to see the sub-agents spawned by each parent session as expendable child rows with their own statuses, so I can tell whether a parent is fanning out / sub-agents' progress.**

**US-4 - Resume from anywhere.**
**As the operator, I want to close my hivemind dashboard and reopen it (in the same pane, a different pane, or a different tmux session) and see the same live state, so the dashboard never feels stateful or fragile.**

**US-5 - Know what hivemind doesn't know.**
**As the operator, I want sessions that are running Claude Code but not reporting telemetry (e.g., started before hooks were installed) to still appear in the dashboard with an explicit `no-telemetry` indicator, so I never miss a session because my dashboard is incomplete when it isn't.**

**US-6 - Switch to a session quickly.**
**As the operator, when I see a session in the dashboard that needs my attention, I want a frictionless way to jump to its tmux pane (e.g., a keystroke that runs the equivalent of `tmux switch-client` + `select-pane`), so the dashboard accelerates rather than replaces my normal tmux workflow.**

### User stories (MVP2 - For forward compatibility)

Listed here so MVP1 design accommodates them, not to be built now:

- **US-M2-1.** Approve or deny a permission prompt directly from the dashboard.
- **US-M2-2.** Send arbitrary input (a message, a slash command) to a session from the dashboard.
- **US-M2-3.** Scope the dashboard to the current tmux session.
- **US-M2-4.** Kill or restart a session from the dashboard.

---

## 3. Functional Requirements

### 3.1 Session discovery

**FR-1.1** hivemind MUST display every Claude Code session running anywhere on the machine, regardless of which tmux session, window, or pane the dashboard itself is launched from.

**FR-1.2** A session that is running Claude Code but has not yet reported any telemetry MUST still appear in the dashboard, with an explicit `no-telemetry` indicator distinguishing it from sessions reporting telemetry.

**FR-1.3** When a Claude Code session terminates (process exits, pane closes), it MUST be removed from the active view within a few seconds.

**FR-1.4** The dashboard MUST tolerate sessions appearing and disappearing during its lifetime without requiring a restart.

### 3.2 Session status

**FR-2.1** Every session row MUST display a derived status from the following enumerated set:

| Status | Meaning |
|---|---|
| `idle` | Session is alive but no agent activity in progress; waiting for user input. |
| `thinking` | Model is generating a response. |
| `tool-running` | A tool call is in progress (Bash, REPL, Read, task, etc.). |
| `awaiting-permission` | A permission prompt is on screen, blocking the session until the user responds. |
| `awaiting-input` | The agent has asked a direct question to the user (e.g., conversational). |
| `errored` | The session has encountered an unhandled error (see FR-2.2). |
| `no-telemetry` | Session is detected but not reporting events (see FR-1.2). |

**FR-2.2** Status MUST update within ~1 second of the underlying event being emitted by Claude Code.

**FR-2.3** The `awaiting-permission` status is the highest-priority signal to the dashboard and MUST be visually distinguishable from all other statuses (color, icon, or both).

### 3.3 Sub-agent visibility

**FR-3.1** Sub-agents (sessions spawned via the `task` tool) MUST be modeled as children of their spawning parent session.

**FR-3.2** Sub-agents MUST render as expendable child rows beneath their parent in the default view.

**FR-3.3** Each sub-agent row MUST display: agent type (e.g., Tasks, PRs?), status (running / completed / errored), and elapsed time since spawn.

**FR-3.4** When a sub-agent completes or errors, its row MUST remain visible under the parent for a short, configurable cool-off window (default ~30 seconds) so the operator can see "what just finished" before it disappears.

### 3.4 Session metadata per row

For each parent Claude Code session, the dashboard MUST display at minimum:

- tmux location (session : window . pane)
- working directory (last path segment, with full path on hover/expand)
- git branch (when applicable)
- model (e.g., `opus-4.7`)
- status (per FR-2.1)
- time since last event

### 3.5 Layout & navigation (MVP1)

**FR-5.1** The default view MUST be a tree grouped by tmux session > Claude sessions > sub-agents.

**FR-5.2** The dashboard MUST support keyboard navigation (arrow keys / 'j'/'k') to move between rows, and expand/collapse of parent rows.

**FR-5.3** The dashboard MUST provide a keystroke to jump the user's tmux client to the focused session's pane (US-6).

**FR-5.4** The dashboard MUST provide a refresh / reconnect keystroke for recovery, and a clean quit keystroke that does not affect any monitored sessions.

**FR-5.5** The data model MUST support a future status-first ("what needs attention") view without requiring schema changes - only a render change.

### 3.6 Multi-client behavior

**FR-6.1** Multiple hivemind dashboards running simultaneously MUST display identical state.

**FR-6.2** Closing a dashboard MUST NOT affect any other running dashboard, nor any monitored session.

**FR-6.3** Opening a fresh dashboard MUST show the current live state immediately, including history that occurred before that dashboard was launched (e.g., a session currently in `awaiting-permission` shows it right away, not just when the event triggers).

### 3.7 Telemetry source plug-ability

**FR-7.1** Claude Code hooks MUST be the primary telemetry source for MVP1.

**FR-7.2** The system MUST be designed so that additional telemetry sources (transcript file parsing, terminal output scraping, hooks from other agents/tools) can be added later without changes to the dashboard UI.

**FR-7.3** Setting up hooks MUST be a single-command operation for the user (e.g., `hivemind install-hooks`), not a manual settings.json edit.

---

## 4. Success Metrics & Acceptance Criteria

### 4.1 Success metrics

How we'll know hivemind is doing its job. These are observational, not instrumented - MVP1 doesn't ship telemetry on itself.

**SM-1 - Tabbing reduction.** The operator's frequency of "tab through panes to check on agents" drops sharply after adopting hivemind. Self-reported, not measured. If the operator still feels compelled to manually check panes, the dashboard isn't surfacing the right signal.

**SM-2 - Permission prompt latency.** Time between a permission prompt appearing to a session and the operator noticing it drops to near-zero when a hivemind dashboard is visible. This is the primary query for MVP1's core product user story and the leading indicator for MVP2 readiness.

**SM-3 - Sub-agent legibility.** When the operator delegates to multiple sub-agents in parallel, they can answer "what's still running and what's dead?" from the dashboard alone, without checking the parent's transcript.

**SM-4 - Dashboard trust.** The operator stops second-guessing the dashboard. If `no-telemetry` rows are showing up frequently, or status updates feel laggy or wrong, trust erodes and the dashboard gets ignored. Sustained use is the core metric.

**SM-5 - Zero-friction multi-client.** The operator opens hivemind in new panes without thinking about it. If launching a second dashboard ever produces inconsistent state or requires reconfiguration, this metric fails.

### 4.2 MVP1 acceptance criteria

The build is done when all of the following hold:

**AC-1 - Discovery & display.**
- [ ] Launching `hivemind` in any tmux pane shows every active Claude Code session on the machine, grouped by tmux session.
- [ ] Sessions started before hivemind was launched appear (with `no-telemetry` if they predate hooks installation).
- [ ] Sessions that exit disappear from the view within a few seconds.

**AC-2 - Status correctness.**
- [ ] Each session displays one of the seven enumerated statuses (FR-2.1).
- [ ] Status updates within ~1 second of the underlying Claude Code event.
- [ ] `awaiting-permission` is visually distinct from every other status at a glance.

**AC-3 - Sub-agents.**
- [ ] Sub-agents render as expendable child rows under their parent.
- [ ] Sub-agent rows show agent type, status, and elapsed time.
- [ ] Completed sub-agents remain visible under the parent for the configured cool-off window before disappearing.

**AC-4 - Multi-client parity.**
- [ ] Running two dashboards in different panes shows identical state.
- [ ] Closing one dashboard does not disturb the other or any monitored session.
- [ ] A freshly opened dashboard immediately reflects the full current state, including events that occurred before it launched.

**AC-5 - Navigation.**
- [ ] Keyboard navigation moves between rows; expand/collapse works on parent rows.
- [ ] A keystroke jumps the user's tmux client to the focused session's pane.
- [ ] Quitting the dashboard does not affect any monitored session.

**AC-6 - Setup.**
- [ ] A single command installs the Claude Code hooks needed for telemetry.
- [ ] First-time setup, end-to-end (install > launch > see a session), takes under five minutes.

**AC-7 - Resilience.**
- [ ] The dashboard tolerates sessions appearing/disappearing without restart.
- [ ] If telemetry temporarily breaks, sessions degrade to `no-telemetry` rather than vanishing or crashing the dashboard.

### 4.3 Out of scope for MVP1 acceptance

Explicitly *not* gating the MVP1 ship:

- Performance under more than ~20 concurrent sessions. We design with this as a soft ceiling but don't formally test beyond.
- Behavior across machine reboots / daemon restarts beyond "doesn't crash and reconnects."
- Polished theming, customization, configuration files. Sensible defaults only.
- Any MVP2 feature (control, scoping, approve-from-dashboard).

---

## 5. MVP2 Preview & Forward-Compatibility Notes

This section is non-binding for MVP1 implementation, but it captures the MVP2 direction so MVP1 design decisions don't accidentally foreclose it.

### 5.1 MVP2 theme: control

MVP1 makes the swarm *legible*. MVP2 makes it *steerable*. The unifying user need: *The operator should be able to act on any session from any dashboard, without tabbing into its pane.*

### 5.2 Anticipated MVP2 features

**MVP2-F1 - Approve / deny permission prompts from the dashboard.**
The flagship MVP2 feature. When a session enters `awaiting-permission`, the dashboard surfaces the prompt's content and offers approve / deny / approve-and-remember actions inline. Resolves the unquoted sandbox pain point that makes parallel work expensive today.

**MVP2-F2 - Send input to a session.**
Type a message, slash command, or response into the dashboard and have it delivered to the focused session as if typed in that pane.

**MVP2-F3 - Status-first ("attention") view.**
A view mode where sessions are grouped by status (`awaiting-permission` first, then `errored`, then active, then idle). Toggle via keystroke. Same data model, different render.

**MVP2-F4 - Scope to current tmux session.**
A keystroke filters the dashboard to only sessions inside the user's current tmux session. Useful when one project is dominating attention.

**MVP2-F5 - Kill / restart sessions.**
Terminate a stuck session or restart it from the dashboard.

**MVP2-F6 - Sub-agent drilldown.**
Expand a sub-agent row to see its current tool call, last message, or recent activity - enough to answer "Is this making progress or stuck?"

### 5.3 Forward-compatibility commitments from MVP1

To keep MVP2 cheap, MVP1 must hold these invariants:

**FC-1 - Bidirectional channel between dashboard and session.**
Even though MVP1 only flows events *from* sessions *to* the dashboard, the underlying transport must support sending instructions back the other way. This is what enables MVP2-F1 and MVP2-F2 without re-architecting.

**FC-2 - Permission prompts are first-class events.**
The `awaiting-permission` status must carry enough context (which tool, what arguments, which session, which sub-agent if applicable) to render a meaningful approve/deny UI later - not just a boolean "blocked."

**FC-3 - Parent/child agent relationships are persisted, not derived at render time.**
Sub-agent rows in MVP1 are real entities with stable identity. MVP2 features like "approve this prompt from PR-3 (the sub-agent of pane-2)" depend on this identity surviving across dashboard reconnects.

**FC-4 - Status-first view requires no schema change.**
MVP1 ships the tree view, but the data model is render-mode agnostic. Adding the attention view in MVP2 is a UI change only.

**FC-5 - Telemetry sources are interchangeable.**
The MVP1 hook adapter is one of N possible adapters. Adding a Codec hook adapter, a transcript-file adapter, or an output scraper is purely additive - no UI changes, no data model changes.

### 5.4 Explicitly deferred past MVP2

Worth noting so we don't accidentally design for them:

- **Cross-machine dashboards, or hivemind in single-machine.** A future "remote hivemind" is a different product.
- **Persistent history / audit log.** MVP1 and MVP2 are about *now*, not *what happened last Tuesday+.
- **Multi-user / shared sessions.** Hivemind is a single-operator tool.
- **Non-Claude-Code agents as first-class peers.** The pluggable telemetry layer makes this *possible*, but unoptimized.
