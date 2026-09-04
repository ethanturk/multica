## Update a single task's status

```bash
NEW_STATE_JSON=$(python3 - "$CURRENT_STATE" "$TASK_ISSUE_ID" "$NEW_STATUS" <<'EOF'
import json, sys

state = json.loads(sys.argv[1])
target_id = sys.argv[2]
new_status = sys.argv[3]

for task in state.get('tasks', []):
    if task['task_issue_id'] == target_id:
        task['status'] = new_status
        break

print(json.dumps(state, indent=2))
EOF
)
```

Then write the updated state back to the master issue as shown above.

---
