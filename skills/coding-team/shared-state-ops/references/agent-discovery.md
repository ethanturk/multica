## Discover a coding-team agent ID by name

All pipeline agents use well-known names. Look them up at runtime to get their IDs for @mentions:

```bash
AGENTS_JSON=$(multica agent list --output json)

get_agent_id() {
  local agents_json="$1"
  local target="$2"
  local result
  result=$(python3 - "$agents_json" "$target" <<'EOF'
import json, sys
agents = json.loads(sys.argv[1])
target = sys.argv[2]
for a in agents:
    if a.get('name') == target:
        print(a['id'])
        sys.exit(0)
sys.exit(1)
EOF
  )
  if [ -z "$result" ]; then
    echo "ERROR: agent '$target' not found in workspace" >&2
    return 1
  fi
  echo "$result"
}

PLANNER_ID=$(get_agent_id "$AGENTS_JSON" "Coding Team Planner")
IMPLEMENTER_ID=$(get_agent_id "$AGENTS_JSON" "Coding Team Implementer")
TESTER_ID=$(get_agent_id "$AGENTS_JSON" "Coding Team Test Writer")
REVIEWER_ID=$(get_agent_id "$AGENTS_JSON" "Coding Team Reviewer")
REFINER_ID=$(get_agent_id "$AGENTS_JSON" "Coding Team Refiner")
PR_WRITER_ID=$(get_agent_id "$AGENTS_JSON" "Coding Team PR Writer")
ORCHESTRATOR_ID=$(get_agent_id "$AGENTS_JSON" "Coding Team Orchestrator")
```

Agent names (exact, case-sensitive):
| Role | Name |
|------|------|
| Orchestrator | `Coding Team Orchestrator` |
| Planner | `Coding Team Planner` |
| Implementer | `Coding Team Implementer` |
| Test Writer | `Coding Team Test Writer` |
| Reviewer | `Coding Team Reviewer` |
| PR Writer | `Coding Team PR Writer` |

---
