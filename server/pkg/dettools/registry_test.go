package dettools_test

import (
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/pkg/dettools"
)

func TestToolCatalogMatchesDaemonDefaults(t *testing.T) {
	t.Parallel()

	names := dettools.AllToolNames()
	if !reflect.DeepEqual(names, daemon.DefaultDetToolsAllowed) {
		t.Fatalf("built-in catalog = %v, daemon defaults = %v", names, daemon.DefaultDetToolsAllowed)
	}

	registry := dettools.NewRegistry(nil)
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			t.Fatalf("duplicate built-in tool %q", name)
		}
		seen[name] = true
		if _, ok := registry.Lookup(name); !ok {
			t.Fatalf("built-in tool %q is not exposed by the unfiltered registry", name)
		}
	}
	if !seen["ui_test_report"] {
		t.Fatal("ui_test_report is not registered")
	}
}
