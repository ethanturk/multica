---
name: Shared State Operations
description: Read and write coding-team pipeline state stored as a JSON block in the Multica master issue description
---

# Coding Team State Operations

All pipeline state lives in the master Multica issue description as a fenced JSON block. Every agent reads the current state, mutates its portion, and writes it back before handing off to the next agent.

Role boundaries are strict: Orchestrator coordinates, never edits repository code, and checks out the repo only in Run 2a to create/verify the shared feature branch; Planner owns the first codebase inspection and never edits; Implementer writes production code and must commit/push before handoff; Test Writer writes tests and must commit/push before handoff; Reviewer reviews and signals; PR Writer creates the draft PR. Never clean or delete a worktree that contains uncommitted or unpushed changes.

Use the `pipeline_state_parse` deterministic tool to parse issue descriptions. Do not reimplement fenced/leading JSON extraction in shell or Python.

Call shape:
```json
{"description":"{issue description text}"}
```

Use `machine_data.state` only when `machine_data.is_pipeline_state == true`. Use `machine_data.config` and `machine_data.body` during Fresh Start when the issue has a config-only JSON block. If `pipeline_state_parse` is unavailable, stop and report that the deterministic tool plane is not enabled.

Use the `coding_comment_extract` deterministic tool to parse coding-team comments, marker ordering, and fenced `json coding-team-artifact` blocks. Downstream roles must prefer `machine_data.artifacts.*` over prose markdown when an artifact exists. If `coding_comment_extract` is unavailable, stop instead of regex-scanning comment markdown.

Determine tool availability from the MCP tools exposed directly to the current
agent run. Do not launch `multica mcp-tools serve` in a shell to inspect
`tools/list`: workspace-authored tools are supplied through task-scoped MCP
configuration, and a manually launched server does not inherit that steps-file
setting, so it will misleadingly list built-ins only. Call the required tool
directly; report it unavailable only when it is absent from the run's tool
surface or the direct call returns a tool-plane error.

---

## Operational routes

Load a reference when its route applies, not all references at startup. If a required reference is missing from the runtime skill bundle, stop and report a skill-packaging blocker; do not reconstruct the procedure from memory.

- When you need the state shape, field meanings, or planning-source invariants, read [State schema](references/state-schema.md).
- When you need to parse or replace the master issue state block, read [Reading and writing state](references/read-write-state.md).
- When you need to mutate one task status safely, read [Updating task status](references/task-status.md).
- When you need to resolve a coding-team agent name to its ID, read [Discovering agent IDs](references/agent-discovery.md).
- When you need to synchronize the shared branch or author a commit, read [Branch sync and commits](references/branch-sync-and-commits.md).

- Before planning, implementing, testing, or reviewing a task, read [Shared delivery and review contract](references/review-contract.md). It owns artifact identity, acceptance evidence, coverage, and repair policy.
