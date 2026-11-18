## Create a task work item

Use `--query id --output tsv` to capture only the new work item's id. **Do not pipe `--output json` into Python or `jq` here** — `az`'s JSON output occasionally contains backslashes (e.g. `System.AreaPath: "ineight\Team"`) that strict JSON parsers reject with `Invalid \escape`. Server-side projection avoids the parse step entirely.

```bash
ADO_ID=$(AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item create \
  --title "{ado_title}" \
  --type "Task" \
  --description "Child of #{deliverable_id}." \
  --area "{area_path}" \
  --iteration "{iteration_path}" \
  --org https://dev.azure.com/incyclesoftware \
  --project ineight \
  --query id \
  --output tsv)
```

`$ADO_ID` is now the integer work-item id. The `ado_title` must be ≤ 50 characters — a concise action phrase only. Never put detailed descriptions, acceptance criteria, or language tags in ADO.

**Idempotency:** verify `$ADO_ID` is non-empty before continuing. If empty (rare — `az` succeeded but returned no id), stop and surface the failure. Do not retry the create blindly — work-item creation is not idempotent and a blind retry will produce duplicates.

---

## Link a task as child of the deliverable

```bash
AZURE_DEVOPS_EXT_PAT=$ADO_PAT_INCYCLE az boards work-item relation add \
  --id {task_ado_id} \
  --relation-type "Parent" \
  --target-id {deliverable_id} \
  --org https://dev.azure.com/incyclesoftware \
  --output json
```

If this fails after the task was created successfully, log a warning and continue — the human can fix the link manually.

---
