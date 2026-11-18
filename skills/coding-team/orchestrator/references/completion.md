## Run 3 - Task Completion Signal

`stage: "implementing"` does not mean the Orchestrator implements code. In this stage, the Orchestrator only reacts to completion signals, updates master state, and starts the next task by mentioning Planner.

If any pending source/test file changes are present in the Orchestrator's checked-out repo during this stage, do not commit, push, clean, or continue implementation. Post a blocking comment on the master issue with `git status --short` output and stop.

After review and refinement pass, Refiner posts:

```text
TASK_COMPLETE
task_issue_id: {id}
status: committed
```

Parse the triggering comment and validate its task identity against the persisted artifacts. Ordinary FAIL routes to Implementer; a decision-needed escalation reaches Orchestrator and must follow the pause guard above, never the completion path.

Update the matching task status in state and write back. If another `pending` task exists, set it `in_progress`, find Planner, and post:

```bash
cat <<COMMENT | multica issue comment add "$NEXT_TASK_ISSUE_ID" --content-stdin
[@Coding Team Planner](mention://agent/${PLANNER_ID})

Please plan this task. The master issue is ${MULTICA_ISSUE_ID}.
COMMENT
```

Only if every task is evidenced `committed`, post summary and ask for push approval. If no pending tasks remain but any task is incomplete or blocked, keep coordinating or preserve the explicit pause; do not emit this completion summary:

```bash
cat <<COMMENT | multica issue comment add "$MULTICA_ISSUE_ID" --content-stdin
All tasks complete: ${COMMITTED} of ${TOTAL} committed successfully.

| Task | Status |
|------|--------|
{for each task: | {task.ado_title} ({if task.ado_id: #{task.ado_id}; else: Multica-only}) | committed/failed |}

Reply **push** to create a draft PR, or **pause** to stop here and resume later.
COMMENT
```

Set `stage: "awaiting_push"` and write state.


If this procedure refers to another Run, load its reference via the main skill router before executing that Run. Global role, pause, and identity guards still apply.
