## Shared Defaults

Master issue config is either a fenced JSON block or the leading top-level JSON object before the prose request. Defaults:

```json
{
  "project": "InEight",
  "code_org": "ineight",
  "code_project": "Platform",
  "repo_name": "AgenticAI",
  "repo_url": "https://dev.azure.com/ineight/Platform/_git/AgenticAI",
  "base_branch": "develop",
  "planning_mode": "auto",
  "planning_source": null,
  "use_existing_tasks": false
}
```

The code repo is defined by the master issue, not inferred from the ADO deliverable. Never hard-code `AgenticAI` except as the backward-compatible default. When storing `repo_url` in state or task issue descriptions, embed `ADO_PAT_INEIGHT` while preserving the configured org/project/repo path. If absent, construct:

```bash
REPO_URL="https://anything:$ADO_PAT_INEIGHT@dev.azure.com/${CODE_ORG}/${CODE_PROJECT}/_git/${REPO_NAME}"
```

Normalize `planning_mode` first: `guided_plan` -> `guided`, `regular_ado_tasks` -> `ado_existing`, and `orchestrator_decomposition` -> `decompose`. Then compute `effective_planning_mode`: use normalized explicit `planning_mode`; otherwise map config `planning_source` with the same aliases; otherwise if `use_existing_tasks == true`, use `ado_existing`; otherwise use `auto`.

`planning_source` in an initial config block is user intent, not pipeline state. If state lacks `stage`, always run Fresh Start; never treat `planning_source: "guided_plan"` as ready or approved state.

**Multica-only mode:** if `deliverable_id` is absent, treat the master issue title/body/comments, minus the config block, as the deliverable context and do not call ADO tools at all. No ADO fetch, child-task load, work-item create/link, Component lookup, or ADO comment posting is allowed. Default to guided planning unless normalized `planning_mode` is `decompose` or `planning_source: "orchestrator_decomposition"` is explicit.

Planning modes:

| Mode | Behavior |
| --- | --- |
| `auto` | With `deliverable_id`, use active ADO child Tasks if present; otherwise guided planning. Without it, guided planning from master issue content. |
| `ado_existing` | Require active ADO child Tasks; block if none exist. |
| `guided` | Coding Team Guided Planner questions the plan one decision at a time, then tasks are created after approval. |
| `decompose` | Automatically decompose the deliverable. |

Accepted aliases: `planning_mode: "guided_plan"` means `guided`; `planning_mode: "regular_ado_tasks"` means `ado_existing`; `planning_mode: "orchestrator_decomposition"` means `decompose`.


If this procedure refers to another Run, load its reference via the main skill router before executing that Run. Global role, pause, and identity guards still apply.
