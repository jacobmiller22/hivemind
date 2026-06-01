# Hivemind Documentation Index

Welcome to the central documentation index for the **hivemind** ecosystem. This repository contains the system requirements, architectural designs, backlog items, and guidelines that govern the development of hivemind.

> [!IMPORTANT]
> **Source of Truth Rule**: Documentation in this directory serves as the definitive source of truth for the codebase. Any architectural, logical, or functional changes proposed or implemented in code **must** be reconciled with and validated against the design and product documentation. 

---

## 1. Documentation Map

The following documents define the various phases and aspects of the project. Developers should refer to and update these files as part of their feature lifecycle.

| Document | Path | Purpose | When to Update |
| :--- | :--- | :--- | :--- |
| **Index & Guidelines** | [INDEX.md](file:///Users/jacobmiller22/projects/hivemind/docs/INDEX.md) | Central entry point, style guidelines, and compliance rules. | When adding new documentation categories or updating standards. |
| **Product Requirements** | [PRD.md](file:///Users/jacobmiller22/projects/hivemind/docs/PRD.md) | High-level goals, user stories, personas, and functional requirements for MVP1 and MVP2. | When features are re-scoped, postponed, or new requirements are introduced. |
| **Technical Design** | [DESIGN.md](file:///Users/jacobmiller22/projects/hivemind/docs/DESIGN.md) | Component architecture, communication protocols (UDS/JSON), schemas, and testing strategy. | Before writing code for any new component, interface change, or schema adjustment. |
| **Project Backlog** | [BACKLOG.md](file:///Users/jacobmiller22/projects/hivemind/docs/BACKLOG.md) | List of planned improvements, UX refinements, bug fixes, and feature candidates. | When a task is completed, reprioritized, or when a new feedback item is logged. |

---

## 2. Architecture & Design Decisions

To keep the client-daemon-adapter system robust and extensible, we adhere to the following workflow for architecture decisions:

1. **Design First**: No non-trivial architectural changes (such as database migrations, socket protocol changes, or new CLI modes) should be implemented without first updating [DESIGN.md](file:///Users/jacobmiller22/projects/hivemind/docs/DESIGN.md).
2. **Backward & Forward Compatibility**: Ensure that telemetry schema modifications are additive and do not break older client/daemon versions. Keep the UDS channel fully bidirectional for MVP2 compatibility.
3. **Graceful Failures**: Telemetry adapters must never crash the parent agent execution. State persistence, connection pooling, and pruning timeouts must be clearly documented.

---

## 3. Style & Visual Standards Guide

To maintain a premium, highly professional documentation standard that matches the aesthetic of our codebase and terminal TUI, we enforce the following rules:

### 3.1. Mandatory Use of Mermaid Diagrams
* **Rule**: All system topology, state transitions, message flows, and component interactions **must** be rendered using standard [Mermaid.js](https://mermaid.js.org/) syntax.
* **Prohibited**: Do not use raw ASCII-art or text-based diagrams, as they are difficult to update, scale poorly, and reduce the premium visual look of the documentation.
* **Mermaid Example**:
  ```mermaid
  graph LR
      Client[TUI Client] <-->|Bidirectional UDS| Daemon[hivemindd]
      Adapter[Hook Adapter] -->|Unidirectional Events| Daemon
  ```

### 3.2. GitHub-Style Alerts
Use alerts strategically to highlight vital context, requirements, or cautions. Do not nest alerts or use them consecutively.
* `> [!NOTE]` — For helpful background or non-blocking implementation details.
* `> [!TIP]` — For performance optimizations, best practices, and developer efficiency.
* `> [!IMPORTANT]` — For essential rules, compliance, and core architectural constraints.
* `> [!WARNING]` — For potential breaking changes, compatibility warnings, or pitfalls.

---

## 4. Code & Documentation Alignment Workflow

Before finalizing any PR or code merge, execute the following reconciliation checklist:

- [ ] **Verify Against PRD**: Ensure the implemented feature meets all functional requirements and satisfies the relevant acceptance criteria in [PRD.md](file:///Users/jacobmiller22/projects/hivemind/docs/PRD.md).
- [ ] **Synchronize with DESIGN.md**: Ensure any types, event structures, or paths in Go/Python align exactly with the schemas in [DESIGN.md](file:///Users/jacobmiller22/projects/hivemind/docs/DESIGN.md).
- [ ] **Update the Backlog**: Mark completed tasks in [BACKLOG.md](file:///Users/jacobmiller22/projects/hivemind/docs/BACKLOG.md) or append follow-up technical debt to the appropriate backlog section.
- [ ] **Validate Tests**: Check that integration and unit tests reflect the documented behavior and output formats.
