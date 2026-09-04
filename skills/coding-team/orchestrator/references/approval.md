## Run 2 - Approval Or Feedback

Read triggering comment and full comment list. If the comment contains `approve` case-insensitively, execute Run 2a.

If the comment is feedback and `planning_source == "guided_plan"`, do not revise the guided tasks from Orchestrator. Set `stage: "guided_planning"`, `planning_status: "questioning"`, append the feedback to `guided_plan.resolved_decisions` or a dedicated feedback note, clear `guided_plan.current_question`, mention Coding Team Guided Planner, and stop:

```bash
AGENTS=$(multica agent list --output json)
GUIDED_PLANNER_ID=$(get_agent_id "$AGENTS" "Coding Team Guided Planner")

cat <<COMMENT | multica issue comment add "$MULTICA_ISSUE_ID" --content-stdin
[@Coding Team Guided Planner](mention://agent/${GUIDED_PLANNER_ID})

Please revise guided planning using the latest approval feedback, then post an updated task proposal.
COMMENT
```

Otherwise treat feedback as non-guided decomposition feedback: re-analyze with the feedback, update/write the revised tasks in state, post a revised Run 1 task breakdown, and keep `stage: "awaiting_approval"`. Preserve the original planning source unless the user explicitly changes it.

### Run 2a - Execute Approved Plan

Idempotency rules:

- If `task.ado_id` exists, reuse it and do not create another ADO work item.
- If `task.task_issue_id` exists, reuse it and do not create another Multica issue.
- After each successful ADO create/link or Multica issue create, immediately write updated state back before continuing.
- If a sub-step fails, stop the loop and post a clear master issue error. Do not blindly retry work-item creation.

For each task in order:

1. If `deliverable_id` exists and `ado_id` is missing, create an ADO Task with `shared-ado-ops` create pattern using `--query id --output tsv`. If `ADO_ID` is empty, stop and report failure. Existing ADO tasks skip this. If `deliverable_id` is absent, skip ADO create/link and leave `ado_id` null/empty.
2. When an ADO Task was created, persist `ado_id`, then link it as a child of the deliverable. If linking fails, log a warning and continue.
3. If missing `task_issue_id`, create a Multica child issue with JSON description. The child issue is routed by the Planner mention below; do not start implementation from the Orchestrator.

```json
{
  "master_issue_id": "{MULTICA_ISSUE_ID}",
  "code_org": "{code_org}",
  "code_project": "{code_project}",
  "repo_name": "{repo_name}",
  "repo_url": "{repo_url}",
  "branch": "{branch}",
  "base_branch": "{base_branch}",
  "ado_id": 67890,
  "ado_title": "...",
  "source": "ado_existing | guided_plan | generated",
  "title": "...",
  "description": "...",
  "acceptance_criteria": ["..."],
  "estimated_language": "csharp"
}
```

````bash
TASK_ISSUE_JSON=$(multica issue create \
  --title "{task.ado_title}" \
  --description-stdin \
  --parent "$MULTICA_ISSUE_ID" \
  --status "todo" \
  --output json <<EOF
```json
{task_issue_description_json}
```
EOF
)
TASK_ISSUE_ID=$(echo "$TASK_ISSUE_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
````

4. Persist `task_issue_id` before the next task.

For non-ADO runs, use `"ado_id": null` in task issue descriptions. Create the remote feature branch from the configured base branch. Branch rules: starts with `feature/`; never `agent/`; if `deliverable_id` exists put it at the end; otherwise put the master issue id at the end; max 50 chars total; lowercase title; strip non-`[a-z0-9 ]`; collapse spaces to `_`; drop filler words `a an the and or of for to with api apis`; use 2-4 distinctive tokens; trim slug tokens from the right if needed, never the id. Example: `feature/enforcement_post_authorize_47358`.

```bash
REPO_PATH=$(multica repo checkout "$REPO_URL")
cd "$REPO_PATH"
git fetch origin
git push origin "origin/${BASE_BRANCH}:refs/heads/${BRANCH}"
git fetch origin "$BRANCH"
if ! git rev-parse --verify "origin/$BRANCH" >/dev/null 2>&1; then
  echo "ERROR: failed to create $BRANCH on remote" >&2
  exit 1
fi
```

Update master state with repo fields, `branch`, `stage: "implementing"`, `planning_status: "approved"`, and all `ado_id`/`task_issue_id` values. Here `implementing` means task-stage execution has started; the Orchestrator must not implement code or edit repository files.

Find Planner with `shared-state-ops` `get_agent_id`, set the first task issue `in_progress`, and post. Run 2a's only task handoff is to Planner; do not mention Implementer, Test Writer, or Reviewer here. Planner posts the implementation plan and then hands off to Implementer.

```bash
cat <<COMMENT | multica issue comment add "$FIRST_TASK_ISSUE_ID" --content-stdin
[@Coding Team Planner](mention://agent/${PLANNER_ID})

Please plan this task. The master issue tracking overall pipeline state is ${MULTICA_ISSUE_ID}.
COMMENT
```

Post master confirmation, omitting the ADO phrase in Multica-only mode:

```bash
cat <<COMMENT | multica issue comment add "$MULTICA_ISSUE_ID" --content-stdin
Tasks approved. Prepared ${N} task(s), {if deliverable_id exists: created or reused their ADO work items, and }created Multica child issues. Branch `${BRANCH}` created from `${BASE_BRANCH}`. Starting with task 1 of ${N}.
COMMENT
```


If this procedure refers to another Run, load its reference via the main skill router before executing that Run. Global role, pause, and identity guards still apply.
