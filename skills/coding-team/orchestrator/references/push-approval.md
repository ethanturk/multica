## Run 4 - Push Approval

Read triggering comment. If it contains `push`, find PR Writer and post:

```bash
cat <<COMMENT | multica issue comment add "$MULTICA_ISSUE_ID" --content-stdin
[@Coding Team PR Writer](mention://agent/${PR_WRITER_ID})

Please create the draft PR. All committed tasks and state are in this issue.
COMMENT
```

If it contains `pause`, post:

```bash
cat <<COMMENT | multica issue comment add "$MULTICA_ISSUE_ID" --content-stdin
Pipeline paused. Reply **push** when ready to create the PR.
COMMENT
```


If this procedure refers to another Run, load its reference via the main skill router before executing that Run. Global role, pause, and identity guards still apply.
