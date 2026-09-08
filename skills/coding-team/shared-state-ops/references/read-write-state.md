## Read state from the master issue

1. Fetch the issue JSON with `multica issue get {master_issue_id} --output json`.
2. Pass its `description` to `pipeline_state_parse`.
3. Use `machine_data.state` when `machine_data.is_pipeline_state == true`; otherwise treat the state as `{}`.

An empty object `{}` means the issue is uninitialized (Orchestrator first run). A config-only JSON block without `stage` must be handled via `machine_data.config`/`machine_data.body`; it is Run 1 input, not resumable state.

If `pipeline_state_parse` is unavailable, stop and report that the deterministic tool plane is not enabled. Do not substitute inline Python, jq, grep, or regex parsing.

---

## Write state to the master issue

Build the updated state JSON in a shell variable or file, then write it back:

The master issue id is a remote Multica record, not a filesystem path. Do not use `Edit`, `Write`, `NotebookEdit`, patch tools, or file editing APIs on `{master_issue_id}`, `$MULTICA_ISSUE_ID`, task issue ids, ADO ids, or UUIDs. State writes must go through `multica issue update` only. If a file-editing tool reports `Could not edit file ... ENOENT`, that means an issue id was treated as a local file; stop and rerun the update with `multica issue update {master_issue_id} --description-stdin`.

```bash
# $NEW_STATE_JSON holds the complete updated state as a JSON string
cat <<ENDDESC | multica issue update {master_issue_id} --description-stdin
\`\`\`json
$NEW_STATE_JSON
\`\`\`
ENDDESC
```

To produce `$NEW_STATE_JSON`, use Python to build and serialize the state dict:

```bash
NEW_STATE_JSON=$(python3 - <<EOF
import json

state = json.loads('''$CURRENT_STATE''')
# ... mutate state ...
state['stage'] = 'implementing'
print(json.dumps(state, indent=2))
EOF
)
```

---
