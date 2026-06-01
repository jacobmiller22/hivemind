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

### 1.1. Tool & Plugin Integrations

Detailed design documents, shims, and architectural integration notes for external developer tool plugins:

| Plugin | Path | Description | Integration Status |
| :--- | :--- | :--- | :--- |
| **Claude Code** | [CLAUDECODE.md](file:///Users/jacobmiller22/projects/hivemind/docs/plugins/CLAUDECODE.md) | Integration path via active lifecycle hooks and passive transcript discovery. | Planned / Conceptual |

---

## 2. Architecture & Design Decisions Workflow

To maintain a clean and maintainable codebase, we adhere to a formal "Design First" process.

> [!WARNING]
> **No Design Leakage in Index**: This index (`INDEX.md`) must not reference or document specific architectural, database, interface, or protocol design details. Because design decisions are subject to frequent change as the codebase evolves, all such technical details belong strictly in [DESIGN.md](file:///Users/jacobmiller22/projects/hivemind/docs/DESIGN.md).

### Design Change Process
1. **Design First**: No non-trivial architectural changes should be implemented in code without first proposing and documenting them in [DESIGN.md](file:///Users/jacobmiller22/projects/hivemind/docs/DESIGN.md).
2. **Review & Reconcile**: Once design changes are agreed upon, all implementation work must align fully with the updated specifications in the design document.

---

## 3. Style & Visual Standards Guide

To maintain a premium, highly professional documentation standard that matches the aesthetic of our codebase and terminal TUI, we enforce the following rules:

### 3.1. Mandatory Use of Mermaid Diagrams
* **Rule**: All system topology, state transitions, message flows, and component interactions **must** be rendered using standard [Mermaid.js](https://mermaid.js.org/) syntax.
* **Prohibited**: Do not use raw ASCII-art or text-based diagrams, as they are difficult to update, scale poorly, and reduce the premium visual look of the documentation.
* **Mermaid Example**:
  ```mermaid
  graph LR
      A[Component A] -->|Message/Event| B[Component B]
  ```

---

## 4. Code & Documentation Alignment Workflow

Before finalizing any PR or code merge, execute the following reconciliation checklist:

- [ ] **Verify Against PRD**: Ensure the implemented feature meets all functional requirements and satisfies the relevant acceptance criteria in [PRD.md](file:///Users/jacobmiller22/projects/hivemind/docs/PRD.md).
- [ ] **Synchronize with DESIGN.md**: Ensure any types, event structures, or paths in Go/Python align exactly with the schemas in [DESIGN.md](file:///Users/jacobmiller22/projects/hivemind/docs/DESIGN.md).
- [ ] **Update the Backlog**: Mark completed tasks in [BACKLOG.md](file:///Users/jacobmiller22/projects/hivemind/docs/BACKLOG.md) or append follow-up technical debt to the appropriate backlog section.
- [ ] **Validate Tests**: Check that integration and unit tests reflect the documented behavior and output formats.
