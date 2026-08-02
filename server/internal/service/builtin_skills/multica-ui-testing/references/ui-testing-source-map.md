# UI-testing source map

Use this evidence layer to verify `SKILL.md` against current implementation.
The project manifest is a repository-owned conventional path, so this checkout
may not contain one; its parser is the linked source of truth.

## Sources

| Contract | Target | Source |
| --- | --- | --- |
| managed_runtime | `server/pkg/uitest/` | [managed runtime package](../../../../../pkg/uitest/) |
| browser_launch_policy | `server/pkg/uitest/upstream.go` | [managed browser launch policy](../../../../../pkg/uitest/upstream.go) |
| browser_network_boundary | `server/pkg/uitest/network_proxy.go` | [loopback-only browser network boundary](../../../../../pkg/uitest/network_proxy.go) |
| daemon_injection | `server/internal/daemon/ui_test_inject.go` | [daemon injection](../../../../daemon/ui_test_inject.go) |
| deterministic_report | `server/pkg/dettools/tool_ui_test_report.go` | [report tool](../../../../../pkg/dettools/tool_ui_test_report.go) |
| ui_test_cli | `server/cmd/multica/cmd_uitest.go` | [install, status, and serve commands](../../../../../cmd/multica/cmd_uitest.go) |
| issue_publication | `server/cmd/multica/cmd_issue.go` | [issue comment attachments](../../../../../cmd/multica/cmd_issue.go) |
| repository_manifest | `.multica/ui-test.json` | [manifest parser and safe inference](../../../../../pkg/uitest/config.go) |
| playwright_regression | `playwright.config.ts` | [repository Playwright configuration](../../../../../../playwright.config.ts) |

## Contract anchors

- `proxy.go` owns the safe tool allowlist and invocation-time rejection.
- `origin.go` owns loopback URL/origin policy.
- `upstream.go` launches managed Chromium through the in-process proxy and
  keeps Playwright allowed origins as defense in depth.
- `network_proxy.go` rejects non-loopback HTTP, WebSocket, and CONNECT
  destinations before DNS or dial.
- `axe.go` exposes the fixed `browser_accessibility_scan`.
- `session.go` owns application, health, setup, browser lifecycle, cleanup, and
  setup-hook environment.
- `tool_ui_test_report.go` derives verdicts, redacts and seals evidence, writes
  report artifacts, and rejects trace/ZIP evidence in schema version 1.
- `cmd_issue.go` implements repeatable `--attachment` and `--content-file`.
