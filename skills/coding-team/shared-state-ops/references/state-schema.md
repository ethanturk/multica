## State schema

```json
{
  "deliverable_id": "12345 or null for direct master-issue requests",
  "code_org": "ineight",
  "code_project": "Platform",
  "repo_name": "AgenticAI",
  "repo_url": "https://anything:ADO_PAT_INEIGHT@dev.azure.com/{code_org}/{code_project}/_git/{repo_name}",
  "base_branch": "develop",
  "branch": "feature/12345_enforcement_post_authorize",
  "stage": "guided_planning | awaiting_approval | implementing | awaiting_push | done",
  "planning_source": "regular_ado_tasks | guided_plan | orchestrator_decomposition",
  "planning_status": "questioning | ready | approved",
  "guided_plan": {
    "status": "questioning | ready",
    "source_context": {
      "deliverable_summary": "...",
      "authoritative_comments": ["..."]
    },
    "current_question": {
      "question": "...",
      "recommended_answer": "...",
      "why_this_matters": "..."
    },
    "answered_questions": [
      {
        "question": "...",
        "recommended_answer": "...",
        "answer": "...",
        "resolution": "..."
      }
    ],
    "resolved_decisions": ["..."],
    "codebase_findings": ["..."]
  },
  "deliverable": {
    "source": "ado | master_issue",
    "title": "...",
    "description": "...",
    "acceptance_criteria": ["..."],
    "area_path": "ineight\\Team",
    "iteration_path": "ineight\\Sprint 42"
  },
  "tasks": [
    {
      "task_issue_id": "multica-abc123",
      "ado_id": 67890,
      "source": "ado_existing | guided_plan | generated",
      "ado_title": "Create integration tests",
      "title": "Integration tests for POST /authorize (C#)",
      "description": "...",
      "acceptance_criteria": ["..."],
      "estimated_language": "csharp",
      "status": "pending | awaiting_clarification | planned | implemented | tested | committed | failed"
    }
  ]
}
```

`planning_source` records how implementation tasks entered the pipeline:

| Value | Meaning |
|-------|---------|
| `regular_ado_tasks` | Planning happened in ADO before Multica started; task objects came from existing non-Done child Task work items. |
| `guided_plan` | The Orchestrator used the ADO deliverable or direct master issue content as guided-planning input; ADO Task work items are created after approval only when `deliverable_id` exists. |
| `orchestrator_decomposition` | The Orchestrator decomposed the deliverable from ADO or direct master issue content. |

`planning_status` is `questioning` while guided planning is asking one decision question at a time, `ready` while the master issue is waiting for task approval, and `approved` after Run 2a has created/reused any applicable ADO task work items and created Multica child issues.

`guided_plan` is optional and may be present when `planning_source == "guided_plan"`. It stores the guided planning conversation and any ADO or codebase findings used to resolve decisions. Canonical task data always lives in `tasks`. A guided run does not require a pre-existing `guided_plan` object in the Multica issue.

If `deliverable_id` is null/absent, the run is Multica-only: do not call ADO, do not create/link ADO work items, do not fetch ADO Component context, and do not post ADO comments. Use the master issue title/body/comments as deliverable context and keep task `ado_id` null/empty.

In Multica-only guided planning, the first Fresh Start run must initialize `stage: "guided_planning"` and ask exactly one guided-planning question before any task synthesis, child issue creation, branch creation, or Planner handoff. A config-only JSON block with `planning_source: "guided_plan"` is not pipeline state unless it also has `stage`.

Task `source` controls ADO creation idempotency when `deliverable_id` exists:
- `ado_existing`: `ado_id` must already be populated; Run 2a skips ADO creation and only creates the Multica task issue if needed.
- `guided_plan`: `ado_id` is empty until Run 2a creates the ADO Task; if `deliverable_id` is null, `ado_id` remains null/empty.
- `generated`: `ado_id` is empty until Run 2a creates the ADO Task; if `deliverable_id` is null, `ado_id` remains null/empty.

The code repository is configured by the master issue and stored in state. `repo_url` must embed `ADO_PAT_INEIGHT` so git operations authenticate without prompts. If the issue supplied `repo_url`, preserve its org/project/repo path and add credentials. If `repo_url` was omitted, build it from `code_org`, `code_project`, and `repo_name`:
```
https://anything:$ADO_PAT_INEIGHT@dev.azure.com/{code_org}/{code_project}/_git/{repo_name}
```

If old issues omit repo fields, default to `code_org: "ineight"`, `code_project: "Platform"`, `repo_name: "AgenticAI"` for backward compatibility.

---
