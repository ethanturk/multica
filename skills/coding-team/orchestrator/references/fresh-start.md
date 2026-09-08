## Run 1 - Fresh Start

Fresh Start guided guard: if `deliverable_id` is absent and config has `planning_source: "guided_plan"`, `planning_mode: "guided"`, `planning_mode: "guided_plan"`, or no explicit decompose request, the only valid Run 1 outcome is `stage: "guided_planning"` plus one Coding Team Guided Planner handoff comment. Do not synthesize tasks in Run 1 for this path.

### 1a. Get Deliverable Context

Parse config from a fenced `json` block if present; otherwise parse the leading top-level JSON object at the start of the description and treat all remaining text as request prose. Do not require the config to be fenced.

If `deliverable_id` exists, fetch the configured ADO deliverable:

```bash
ITEM=$(AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item show --id "$DELIVERABLE_ID" --org https://dev.azure.com/incyclesoftware --output json)
COMMENTS=$(curl -sS -u ":$ADO_PAT_INCYCLE" -H "Content-Type: application/json" \
  "https://dev.azure.com/incyclesoftware/ineight/_apis/wit/workItems/${DELIVERABLE_ID}/comments?api-version=7.1-preview.4")
```

Extract title, stripped description, stripped/split acceptance criteria, area path, iteration path, and comments as `[{author, created_date, text}]` oldest to newest.

If `deliverable_id` is absent, do not run the ADO commands above. Use the master issue title plus non-config body text as `deliverable.title`/`description`, empty `acceptance_criteria` unless clear checklist criteria are present, empty area/iteration, and master issue comments as authoritative comments. Comments are authoritative over earlier text when they narrow, expand, or contradict it.

### 1b. Choose Planning Source

Read master issue description/comments. Only read active child ADO Task work items when `deliverable_id` exists:

```bash
MASTER_JSON=$(multica issue get "$MULTICA_ISSUE_ID" --output json)
MASTER_COMMENTS=$(multica issue comment list "$MULTICA_ISSUE_ID" --output json)
```

If `deliverable_id` exists, keep child work items whose `System.WorkItemType` is `Task` and state is not Done/Closed. If it is absent, skip child lookup and stay Multica-only. Guided planning uses the deliverable fields/comments from 1a; never ask the user to add `## Guided Plan Ready`, a guided-planning artifact, or fenced JSON task breakdown for `guided`, `auto`, or config `planning_source: "guided_plan"`. If requirements are unusable, report that problem directly.

Planning source precedence:

| Condition | Source | Action |
| --- | --- | --- |
| no `deliverable_id` and explicit decompose | `orchestrator_decomposition` | Decompose master issue content; create Multica child issues only. |
| no `deliverable_id` otherwise | `guided_plan` | Start guided planning from master issue content; create Multica child issues only after approval. |
| `effective_planning_mode == "ado_existing"` | `regular_ado_tasks` | Load active ADO child Tasks, or block. |
| `effective_planning_mode == "guided"` | `guided_plan` | Start guided planning. |
| `effective_planning_mode == "decompose"` | `orchestrator_decomposition` | Auto-decompose. |
| `effective_planning_mode == "auto"` and active child Tasks exist | `regular_ado_tasks` | Load active ADO child Tasks. |
| `effective_planning_mode == "auto"` and no active child Tasks exist | `guided_plan` | Start guided planning. |

Only `ado_existing` may block for missing child Tasks. Post and stop without setting `awaiting_approval`:

```bash
cat <<'COMMENT' | multica issue comment add "$MULTICA_ISSUE_ID" --content-stdin
## Existing ADO Tasks Needed

This issue is configured for {effective_planning_mode}, which requires existing ADO child Task work items.

No active ADO child Task work items were found under deliverable {deliverable_id}.

Create or reopen the ADO child tasks, then mention or assign Coding Team Orchestrator to resume. Alternatively, update the issue config to use `planning_mode: "guided"` or `planning_mode: "auto"` so tasks can be created from the ADO deliverable.
COMMENT
```

### 1c. Build Tasks or Start Guided Planning

For `regular_ado_tasks`, create task objects from each active ADO child Task:

```json
{
  "source": "ado_existing",
  "ado_id": 123,
  "ado_title": "verbatim ADO title",
  "title": "same as ado_title",
  "description": "stripped ADO description",
  "acceptance_criteria": [],
  "estimated_language": "unknown",
  "status": "pending"
}
```

For `orchestrator_decomposition`, produce 2-6 independently implementable/testable tasks covering all acceptance criteria. Each task has:

- `ado_title`: concise action phrase <= 50 chars; no language tags or prohibited mentions.
- `title`: detailed local title with language/scope hints.
- `description`: 2-4 implementation sentences.
- `acceptance_criteria`: task-specific, testable criteria.
- `estimated_language`: `python`, `csharp`, or `unknown`.
- `source`: `generated`; `status`: `pending`.

For `guided_plan`, do not produce tasks or child issues yet, even if the work looks obvious. Initialize and write state before asking:

```json
{
  "stage": "guided_planning",
  "planning_source": "guided_plan",
  "planning_status": "questioning",
  "tasks": [],
  "guided_plan": {
    "status": "questioning",
    "source_context": "summary of ADO or master-issue deliverable fields and authoritative comments",
    "answered_questions": [],
    "resolved_decisions": [],
    "domain_glossary": [],
    "adr_candidates": [],
    "codebase_findings": [],
    "current_question": {}
  }
}
```

Guided planning follows Grill-with-docs through the separate Coding Team Guided
Planner. The Orchestrator only initializes state and routes the master issue to
that agent. Do not ask guided-planning questions from Orchestrator, and do not
check out or inspect the repository during guided planning.

Set the master issue `in_progress`, mention the Guided Planner, then stop:

```bash
multica issue status "$MULTICA_ISSUE_ID" in_progress
AGENTS=$(multica agent list --output json)
GUIDED_PLANNER_ID=$(get_agent_id "$AGENTS" "Coding Team Guided Planner")

cat <<COMMENT | multica issue comment add "$MULTICA_ISSUE_ID" --content-stdin
[@Coding Team Guided Planner](mention://agent/${GUIDED_PLANNER_ID})

Please run guided planning for this master issue. Ask one question at a time, record resolved terms and decision notes, and propose tasks when ready.
COMMENT
```

Hard stop: after posting the Guided Planner handoff, do not continue to task
synthesis, child issue creation, branch creation, Planner handoff, or
implementation in the same run.

### 1d. Post Proposed Tasks

For non-guided sources, or after Coding Team Guided Planner completes guided planning, write state with `stage: "awaiting_approval"`, `planning_source`, `planning_status: "ready"`, and task objects. Preserve `ado_id` for existing ADO tasks; generated/guided ADO-backed tasks leave `ado_id` empty until approval; Multica-only tasks keep `ado_id` null/empty.

Post:

```bash
cat <<'COMMENT' | multica issue comment add "$MULTICA_ISSUE_ID" --content-stdin
## Proposed Tasks for: {deliverable.title}

Planning source: {planning_source}

{for each task, numbered:}
**{n}. {task.ado_title}** ({if task.ado_id exists: existing ADO #{task.ado_id}; else if deliverable_id exists: will appear in ADO; else: Multica-only})
Local title: {task.title}
Language: {task.estimated_language}
Source: {task.source}

Description: {task.description}

Acceptance criteria:
{- each criterion}

---

Reply **approve** to proceed, or provide feedback to revise the breakdown.

```json coding-team-artifact
{
  "artifact_type": "task_set",
  "artifact_version": 1,
  "master_issue_id": "${MULTICA_ISSUE_ID}",
  "planning_source": "{planning_source}",
  "tasks": [{json task objects with title, description, acceptance_criteria, estimated_language, source, ado_id}]
}
```
COMMENT
```

Set master issue status `in_progress`.


If this procedure refers to another Run, load its reference via the main skill router before executing that Run. Global role, pause, and identity guards still apply.
