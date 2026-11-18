package uitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const maxAxeScriptBytes = 1024 * 1024

var accessibilityScanTool = toolDescriptor{
	Name:        "browser_accessibility_scan",
	Description: "Run a fixed Axe scan of the current page. Returns violation IDs, impact, help, affected selectors, and failure summaries. Does not accept JavaScript.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
}

func readManagedAxe(files runtimeFiles) ([]byte, error) {
	file, err := openRegularManagedFile(files.Axe)
	if err != nil {
		return nil, fmt.Errorf("open managed Axe: %w", err)
	}
	defer file.Close()
	script, err := io.ReadAll(io.LimitReader(file, maxAxeScriptBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read managed Axe: %w", err)
	}
	if len(script) > maxAxeScriptBytes {
		return nil, fmt.Errorf("managed Axe exceeds 1 MiB")
	}
	if len(script) == 0 {
		return nil, fmt.Errorf("managed Axe is empty")
	}
	return script, nil
}

func axeEvaluateParams(script []byte) (json.RawMessage, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(string(script)); err != nil {
		return nil, fmt.Errorf("encode fixed Axe source: %w", err)
	}
	quotedScript := bytes.TrimSpace(encoded.Bytes())
	function := `async () => {
  const source = ` + string(quotedScript) + `;
  (0, eval)(source);
  const result = await globalThis.axe.run(document, { resultTypes: ["violations"] });
  const clip = (value, limit) => String(value ?? "").slice(0, limit);
  return result.violations.slice(0, 25).map((violation) => ({
    id: clip(violation.id, 200),
    impact: clip(violation.impact, 100),
    help: clip(violation.help, 500),
    helpUrl: clip(violation.helpUrl, 1000),
    nodes: violation.nodes.slice(0, 10).map((node) => ({
      target: node.target.slice(0, 5).map((selector) => clip(selector, 300)),
      failureSummary: clip(node.failureSummary, 1000)
    }))
  }));
}`
	arguments, err := json.Marshal(map[string]string{"function": function})
	if err != nil {
		return nil, fmt.Errorf("encode fixed Axe evaluation: %w", err)
	}
	params, err := json.Marshal(toolCallParams{
		Name:      "browser_evaluate",
		Arguments: arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("encode fixed Axe call: %w", err)
	}
	if len(params) > maxRPCFrameBytes {
		return nil, errRPCFrameTooLarge
	}
	return params, nil
}
