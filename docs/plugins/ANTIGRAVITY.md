# Antigravity Integration Guide

This document explores the architectural design, compatibility, and integration paths for connecting the **Antigravity** family of AI developer tools with the **hivemind** monitoring daemon (`hivemindd`).

> [!NOTE]
> **Integration Philosophy**: To keep `hivemindd` generic and extensible, integrations with tools like Antigravity are structured as pluggable telemetry shims or log-parsing adapters. These adapters map native tool lifecycles onto standard Hivemind events (`session_started`, `status_changed`, etc.) without altering the core daemon architecture.

---

## 1. Architectural Compatibility Summary

The Antigravity product suite comprises three distinct products. Because each product operates on a different runtime environment and interface paradigm, their compatibility with `hivemind` varies significantly. 

> [!WARNING]
> **Product Compatibility Restriction**: As of the current version, the **Antigravity 2.0 Agent Manager** and **Antigravity IDE** are **not compatible** with the Hivemind state daemon. There are currently no plans to integrate them into the Hivemind ecosystem. Integration effort is exclusively focused on the terminal-native **Antigravity CLI**.

### Compatibility & Integration Matrix

| Product | Architecture Paradigm | Active Telemetry (Push) | Passive Telemetry (Pull/Logs) | Integration Strategy |
| :--- | :--- | :--- | :--- | :--- |
| **Antigravity CLI** | Terminal-native AI coding agent | **Unsupported / Unknown** (No middleware/hook hooks exposed) | **Fully Supported** (Watching local transcript session files) | Active monitoring via log discovery and tailing of local session databases. |
| **Antigravity 2.0 Agent Manager** | GUI dashboard & orchestrator | **Not Compatible** | **Not Compatible** | **No Integration Planned** |
| **Antigravity IDE** | Desktop IDE integration environment | **Not Compatible** | **Not Compatible** | **No Integration Planned** |

---

## 2. Active Telemetry: Push-Based Hooks

Unlike Claude Code, which provides a native hooks middleware system (`~/.claude/settings.json`), **Antigravity CLI** currently does **not** expose public, configurable execution hooks or interceptors that run external scripts during its lifecycle. 

Consequently, push-based active telemetry is **unsupported** or **unknown** for Antigravity CLI at this time.

```mermaid
graph TD
    subgraph Antigravity CLI Process
        A[User Input] --> B[Model Planning]
        B --> C[Tool Execution]
        C --> D[Model Response]
    end
    
    subgraph hivemindd Ingest
        E[UDS Socket Receiver]
    end

    B -.->|No execution hooks| E
    C -.->|No middleware intercepts| E
    
    style B fill:#555,stroke:#333,stroke-dasharray: 5 5
    style C fill:#555,stroke:#333,stroke-dasharray: 5 5
    style E fill:#8b2635,stroke:#5c1d24
```

---

## 3. Passive Telemetry: Pull-Based Log Discovery

For Antigravity CLI, **Passive Telemetry (Pull-Based)** is the primary and highly deterministic integration pathway. The CLI automatically persists comprehensive, step-by-step logs and conversation transcripts to the local filesystem.

```mermaid
graph LR
    subgraph "Antigravity CLI Storage (~/.gemini/antigravity-cli/)"
        A["brain/"] --> B["conv_id_A/"]
        A --> C["conv_id_B/"]
        B --> D[".system_generated/logs/transcript.jsonl"]
        B --> E[".system_generated/logs/transcript_full.jsonl"]
    end

    subgraph "State Daemon (hivemindd)"
        Daemon["hivemindd Ingestion Server"] -->|"Discovers & Tails"| D
        Daemon -->|"Parses Step JSON"| DB[(Flat Session State DB)]
    end
```

### 3.1. Transcript Location & Structure

* **Base Configuration Directory**: `~/.gemini/antigravity-cli/`
* **Session Directory Path**: `~/.gemini/antigravity-cli/brain/<conversation-id>/.system_generated/logs/`
* **Target Transcript Files**:
  * `transcript.jsonl`: Contains the chronological list of user inputs, tool invocations, and planner responses.
  * `transcript_full.jsonl`: Full detailed log containing complete tool payloads and execution steps.

### 3.2. Log Format Specification

The transcript files are structured in JSON Lines (JSONL) format. Each line represents a discrete state transition or step in the AI session lifecycle.

#### Example Event Entry (User Input)
```json
{
  "step_index": 0,
  "source": "USER_EXPLICIT",
  "type": "USER_INPUT",
  "status": "DONE",
  "created_at": "2026-06-01T15:59:46Z",
  "content": "<USER_REQUEST>\nhello\n</USER_REQUEST>"
}
```

#### Example Event Entry (Planner Tool Execution Request)
```json
{
  "step_index": 2,
  "source": "MODEL",
  "type": "PLANNER_RESPONSE",
  "status": "DONE",
  "created_at": "2026-06-01T15:59:46Z",
  "content": "I can help you analyze the workspace directory...",
  "tool_calls": [
    {
      "name": "list_dir",
      "args": {
        "DirectoryPath": "\"/Users/jacobmiller22/projects/hivemind\"",
        "toolAction": "\"/Listing the root workspace directory\"",
        "toolSummary": "\"/Workspace listing\""
      }
    }
  ]
}
```

---

## 4. State Parsing & Recovery Heuristics

A specialized `AntigravityPassiveAdapter` can tail and parse these JSONL transcripts to seamlessly project active sessions onto the Hivemind dashboard.

### 4.1. Lifecycle Mapping Rules

To determine the `DerivedStatus` of an Antigravity CLI session, the adapter tracks the latest parsed line in the active `transcript.jsonl`:

| Last Line `type` / Condition | Last Line `status` | Derived Status | Explanation |
| :--- | :--- | :--- | :--- |
| `USER_INPUT` | `DONE` | `thinking` | The user submitted a prompt; the agent is currently planning. |
| `PLANNER_RESPONSE` (contains `tool_calls`) | `DONE` | `tool-running` | The agent decided to execute a tool. |
| `PLANNER_RESPONSE` (no `tool_calls`) | `DONE` | `idle` | The agent completed its response turn and is waiting for user input. |
| Any step | `ERROR` | `errored` | The process encountered an unhandled system failure. |

### 4.2. Context Extraction

* **Session Identity**: Mapped directly from the parent folder's `<conversation-id>` UUID in the path `brain/<conversation-id>`.
* **CWD (Current Working Directory)**: Extracted by examining file path arguments in recent tool calls (e.g., `list_dir` or `view_file`) or parsing early command execution contexts.
* **Last Activity**: Determined using the `created_at` timestamp of the latest event inside `transcript.jsonl` or the last-modified metadata of the log file itself.

---

## 5. Integration Feasibility Matrix

The table below highlights how specific Antigravity CLI behaviors map to the state representation requirements of `hivemind`:

| Antigravity CLI Feature | Integration Feasibility | Design Strategy |
| :--- | :--- | :--- |
| **Interactive Tool Calls** | **Highly Feasible** | The passive adapter flags transitions to `tool-running` based on `PLANNER_RESPONSE` containing `tool_calls`. |
| **Subagent Management** | **Feasible** | When subagents are spawned (documented via specific tool invocations in the transcript), the adapter registers them in the session's Sub-agent Registry. |
| **Active Telemetry Hook** | **Not Feasible** | Currently blocked by lack of native extension hooks in the CLI. |

---

## 6. Implementation Roadmap

To officially support Antigravity CLI in the Hivemind ecosystem:

1. **Add `AntigravityPassiveAdapter` to `hivemindd`**:
   * Implement directory polling for `~/.gemini/antigravity-cli/brain/`.
   * Parse `transcript.jsonl` files in real-time as updates are written by the CLI process.
2. **Implement State Coexistence / Recovery**:
   * Map discovered directories to logical Session records.
   * Project status changes onto the terminal TUI dynamically using last-modified timestamps.
3. **Verify via Passive File Test Framework**:
   * Verify that cold-starting the daemon correctly recovers past sessions under the correct conversation IDs.
   * Assert that new steps written to the logs dynamically update the session's state and `lastActivity` fields.
