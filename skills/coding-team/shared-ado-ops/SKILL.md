---
name: Shared ADO Operations
description: Azure DevOps CLI patterns shared across all coding-team agents — two separate ADO instances for work items vs. code
---

# ADO Operations Reference

Operations span **two separate ADO instances**. Always prefix `az` calls with the correct PAT inline so neither variable bleeds into the other:

| Instance | Org URL | Project | PAT env var | Used for |
|----------|---------|---------|-------------|----------|
| incyclesoftware | `https://dev.azure.com/incyclesoftware` | `ineight` | `ADO_PAT_INCYCLE` | Work items, boards, comments |
| code repo | `https://dev.azure.com/{code_org}` | `{code_project}` | `ADO_PAT_INEIGHT` | Git repos, pull requests |

Prefix pattern:
```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards ...   # work items
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INEIGHT  az repos ...   # PRs
```

**Do not use `az rest` for ADO endpoints.** It tries to acquire an Azure AD token and fails against `dev.azure.com` (you'll see `Can't derive appropriate Azure AD resource from --url` followed by an HTML sign-in page). Use `curl` with Basic auth and the PAT instead. The PAT goes in the password slot with an empty username:
```bash
curl -sS -u ":$ADO_PAT_INCYCLE" -H "Content-Type: application/json" "<uri>"
```

For git operations, embed the PAT in the URL — never print it:
```bash
REPO_URL="https://anything:$ADO_PAT_INEIGHT@dev.azure.com/${CODE_ORG}/${CODE_PROJECT}/_git/${REPO_NAME}"
```

`CODE_ORG`, `CODE_PROJECT`, and `REPO_NAME` come from the master issue state. Old issues may omit them; use `ineight`, `Platform`, and `AgenticAI` as backward-compatible defaults.

Use the `ado_payload_normalize` deterministic tool after fetching ADO JSON to normalize supplied payloads. It does not call ADO. It only converts already-fetched work items, comment responses, child-item batches, and ancestor arrays into plain text fields the planning skills can consume:

```json
{
  "work_item": {},
  "comments_response": {},
  "child_items_response": {},
  "ancestors": []
}
```

Use its `machine_data.work_item.description`, `machine_data.work_item.acceptance_criteria`, `machine_data.comments`, `machine_data.active_child_tasks`, and `machine_data.nearest_component` instead of repeating ad hoc HTML stripping and active-task filtering. If `ado_payload_normalize` is unavailable, stop and report that the deterministic tool plane is not enabled.

---

## Operational routes

- When you need to fetch a work item, its comments, its active children, or its ancestors, read [Reading work items](references/work-item-reading.md).
- When you need to create an ADO Task or link it beneath a deliverable, read [Creating and linking tasks](references/task-creation.md).
- When you need to post a work-item comment safely, read [Posting comments](references/comments.md).

## Hard rules

- Never run `az boards work-item update --state ...` on any work item. Board state is owned by the human.
- Never send the task's detailed `title`, `description`, or `acceptance_criteria` to ADO. Those fields live only in the Multica master issue state.
- Never mention AI, agents, or automation in any ADO work item title, description, or comment.
