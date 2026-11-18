## Fetch a work item

```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item show \
  --id {id} \
  --org https://dev.azure.com/incyclesoftware \
  --output json
```

Key fields under `.fields`:
- Title: `System.Title`
- Description (HTML): `System.Description`
- Acceptance criteria (HTML): `Microsoft.VSTS.Common.AcceptanceCriteria`
- Area path: `System.AreaPath`
- Iteration path: `System.IterationPath`
- Work item type: `System.WorkItemType`
- State: `System.State`

Pass the raw work item JSON to `ado_payload_normalize` and use the normalized plain-text fields. Do not strip HTML or split acceptance criteria in the skill.

---

## Fetch work item comments

```bash
curl -sS -u ":$ADO_PAT_INCYCLE" \
  -H "Content-Type: application/json" \
  "https://dev.azure.com/incyclesoftware/ineight/_apis/wit/workItems/{id}/comments?api-version=7.1-preview.4"
```

The response has a `.value` array. Pass the full response to `ado_payload_normalize` and use `machine_data.comments`, ordered oldest → newest.

---

## Fetch child work items of a deliverable

Fetch with relations expanded:
```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item show \
  --id {deliverable_id} \
  --expand relations \
  --org https://dev.azure.com/incyclesoftware \
  --output json
```

Filter `.relations[]` where `.rel == "System.LinkTypes.Hierarchy-Forward"`. The child ID is the trailing path segment of `.url` (after `/workItems/`).

Batch-fetch child details:
```bash
cat > /tmp/ado_batch.json <<'EOF'
{"ids":[{comma-separated ids}],"fields":["System.Id","System.Title","System.Description","Microsoft.VSTS.Common.AcceptanceCriteria","System.State"]}
EOF

curl -sS -u ":$ADO_PAT_INCYCLE" \
  -H "Content-Type: application/json" \
  -X POST \
  --data-binary @/tmp/ado_batch.json \
  "https://dev.azure.com/incyclesoftware/_apis/wit/workitemsbatch?api-version=7.1"
```

Skip any child whose `System.State` is `Done` or `Closed`.

---

## Fetch parent/ancestor work items

Use this when a task or deliverable needs broader ADO context, such as the owning **Component**. Do not assume a fixed hierarchy depth: the Component might be the direct parent of the deliverable, or it might be a parent of a parent.

Fetch each candidate work item with relations expanded:
```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item show \
  --id {work_item_id} \
  --expand relations \
  --org https://dev.azure.com/incyclesoftware \
  --output json
```

Follow `System.LinkTypes.Hierarchy-Reverse` parent links upward, collecting the fetched parent work item JSON objects in order. Pass the ordered array as `ancestors` to `ado_payload_normalize` and use `machine_data.nearest_component` plus the normalized ancestor chain. If no Component is found within 10 parent hops, continue with deliverable/task context and note that no ADO Component was found; do not fail planning solely because the hierarchy is missing or irregular.

---
