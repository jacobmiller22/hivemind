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

## UI & Aesthetics

- [ ] **Custom Color Themes**: Add support for passing custom ANSI/hex color themes via an optional configuration file or CLI flags.
- [ ] **Tree Node Expansion Memory**: Persist the expanded/collapsed state of specific tmux session nodes across client reconnects.
