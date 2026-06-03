# Hivemind Backlog

This document tracks planned improvements, UX refinements, and future roadmap items for the **hivemind** ecosystem.

## CLI & Installation Enhancements

### Verbose Installation Output
- [ ] **Improve `install-hooks` feedback**: Modify the `hivemind install-hooks` command to produce more detailed output describing precisely what actions it is performing.
  - **Proposed Output Details**:
    - Print the determined user home directory.
    - Show checks for existing plugin directories (e.g. `~/.gemini/config/plugins/hivemind_hooks`).
    - Detail the exact copy operations, including source and target paths.
    - List all shell configuration files analyzed (e.g., `.zshrc`, `.bashrc`, `.bash_profile`).
    - Explicitly state whether the file was skipped (because `PYTHONPATH` was already configured) or modified.
    - Outline the exact line(s) appended for each shell profile.
    - Provide a clear post-install verification check list.

## Core Features & Daemon

- [ ] **Daemon Auto-pruning Optimization**: Refine the cooldown timer logic to allow dynamic adjustments to the sub-agent cool-off pruning window (default 30 seconds).
- [ ] **Connection Retry Backoff**: Implement exponential backoff in the client subscription routine when attempting to reconnect to the Unix Domain Socket (UDS).
- [ ] **Reconcile Event Names and Decouple from Antigravity**: Standardize the daemon event schema to make it fully tool-agnostic.
  - **Goal**: Avoid modeling `hivemind` event names directly after the lifecycles/hooks of any single tool (e.g., Antigravity SDK). Instead, define a unique, unified set of `hivemind` event names and use tool-specific hook adapters to translate/map their native events into the standard `hivemind` protocol.
- [ ] **Awaiting User Input/Permission Detection for Interactive Tools**: Capture and reflect when an agent is blocked awaiting user permission/input on interactive tools (e.g., `run_command` prompting for approval).
  - **The Problem**: Currently, when an agent is blocked on a permission prompt (e.g., `needs approval for Bash`), no tool execution step is written to `transcript.jsonl` yet. The last line remains the parent `PLANNER_RESPONSE` step, causing the daemon to heuristically report the session as `THINKING` or `RUNNING TOOL` instead of the true `AWAITING_PERMISSION` or `AWAITING_INPUT` state.
  - **Proposed Solutions**:
    - **Active Lockfile/State File Polling**: Scan for temporary permission lockfiles or state indicators (e.g., `.permission_pending`) written by active shims or terminal shims under the session directory.
    - **Shim Event Stream Adapter**: Update active shims/hooks to push a `permission_requested` event directly to the daemon's Unix Domain Socket (UDS) as soon as the CLI displays the permission block.
    - **Predictive Step Heuristics**: Modify the daemon's passive transcript parser to temporarily project `AWAITING_PERMISSION` when a `PLANNER_RESPONSE` specifies tools configured to require user permission, until a corresponding tool execution output step is written.
- [ ] **UDS Connection Keep-Alive & Debouncing**: Optimize UDS connection handling to avoid opening and closing a new socket connection for every telemetry event, particularly when the agent is rapidly spawning `./hivemind event` commands.
  - **The Challenge**: Since `./hivemind` is a transient CLI process spawned as a hook, it opens a connection, sends the event, and exits. This incurs setup/teardown overhead and connection thrashing.
  - **Proposed Solutions**:
    - **Persistent Telemetry Proxy/Multiplexer**: Explore running a lightweight daemon thread or using named pipes/FIFOs that keep a single long-lived UDS socket connection open, allowing short-lived CLI calls to write to a fast local buffer.
    - **Direct Agent SDK/Shim Integration**: Integrate a persistent background telemetry client directly into the agent runner's native codebase (Go or Python) rather than shelling out to transient CLI commands.

## UI & Aesthetics

- [ ] **Custom Color Themes**: Add support for passing custom ANSI/hex color themes via an optional configuration file or CLI flags.
- [ ] **Tree Node Expansion Memory**: Persist the expanded/collapsed state of specific tmux session nodes across client reconnects.
