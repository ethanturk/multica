package detsteps

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/pkg/dettools"
)

// StepDef is a workspace-authored deterministic tool delivered to the per-task
// MCP server: the name an agent calls, a description, and the Go source run in
// the sandbox. The daemon writes a JSON array of these into the task work dir
// and points the server at it via MULTICA_DETTOOLS_STEPS_FILE.
type StepDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// stepInputSchema advertises a permissive object: a step receives the decoded
// arguments as map[string]any, so any JSON object is valid input.
var stepInputSchema = json.RawMessage(`{"type":"object","additionalProperties":true}`)

// Tools converts step definitions into dettools.Tool values whose handlers run
// the step source in the sandbox and return the standard Result envelope.
func Tools(steps []StepDef) []dettools.Tool {
	out := make([]dettools.Tool, 0, len(steps))
	for _, s := range steps {
		out = append(out, stepTool(s))
	}
	return out
}

func stepTool(s StepDef) dettools.Tool {
	source := s.Source // capture per definition
	desc := strings.TrimSpace(s.Description)
	if desc == "" {
		desc = "Workspace-authored deterministic tool."
	}
	return dettools.Tool{
		Name:        s.Name,
		Description: desc,
		InputSchema: stepInputSchema,
		Handler: func(ctx context.Context, args json.RawMessage, env dettools.ToolEnv) dettools.Result {
			var input map[string]any
			if len(strings.TrimSpace(string(args))) > 0 {
				if err := json.Unmarshal(args, &input); err != nil {
					return dettools.Errf(dettools.CodeInvalidInput, "invalid tool input: %v", err)
				}
			}
			return Run(ctx, source, input, Options{Timeout: env.Timeout})
		},
	}
}

// LoadStepsFile reads a JSON array of StepDef from path. An empty path yields no
// steps and no error (the tool plane simply has no authored tools).
func LoadStepsFile(path string) ([]StepDef, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var steps []StepDef
	if err := json.Unmarshal(data, &steps); err != nil {
		return nil, err
	}
	return steps, nil
}
