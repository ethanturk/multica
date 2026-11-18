## Run 1G - Route Guided Planning

If the master issue is already in `guided_planning`, route the current turn to
Coding Team Guided Planner. Do not inspect ADO, ask questions, synthesize tasks,
or process approval from this stage.

Resolve the Guided Planner and post:

```bash
AGENTS=$(multica agent list --output json)
GUIDED_PLANNER_ID=$(get_agent_id "$AGENTS" "Coding Team Guided Planner")

cat <<COMMENT | multica issue comment add "$MULTICA_ISSUE_ID" --content-stdin
[@Coding Team Guided Planner](mention://agent/${GUIDED_PLANNER_ID})

Please continue guided planning for this master issue.
COMMENT
```

Then stop. Guided-plan tasks need ADO Task work items only when `deliverable_id`
exists; Multica-only runs skip ADO and create child issues only after approval.


If this procedure refers to another Run, load its reference via the main skill router before executing that Run. Global role, pause, and identity guards still apply.
