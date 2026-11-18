## Post a comment to a work item

Write the body to a temp file to avoid shell quoting issues:

```bash
python3 -c "
import json, sys
issues = sys.argv[1:]
items = ''.join(f'<li>{i}</li>' for i in issues)
payload = {'text': f'<ul>{items}</ul>'}
print(json.dumps(payload))
" "Issue one" "Issue two" > /tmp/ado_comment.json

curl -sS -u ":$ADO_PAT_INCYCLE" \
  -H "Content-Type: application/json" \
  -X POST \
  --data-binary @/tmp/ado_comment.json \
  "https://dev.azure.com/incyclesoftware/ineight/_apis/wit/workItems/{ado_id}/comments?api-version=7.1-preview.4"
```

Format the `text` field as an HTML fragment (`<ul><li>...</li></ul>`) — ADO renders it as rich text.

---
