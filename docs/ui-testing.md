# UI testing

Multica agents can audit a local web UI, add persistent Playwright regression
coverage, or do both from an ordinary assigned issue. V1 uses managed headless
Chromium and loopback targets.

## Install and check status

Install the pinned Playwright MCP, Axe, Playwright, and Chromium runtime
explicitly:

```bash
multica ui-test install
multica ui-test status
multica ui-test status --output json
```

Status is `unavailable`, `installing`, `ready`, or `broken`. Assignment never
downloads a browser. A compatible daemon injects `multica-ui-test` only when
the pinned runtime is `ready`; browser and application processes start lazily
on the first managed browser action.

## Repository contract

Add `.multica/ui-test.json` when conservative inference is ambiguous:

```json
{
  "start": "make start",
  "url": "http://127.0.0.1:3000",
  "health": "/login",
  "setup": "pnpm ui-test:setup",
  "viewport": {
    "width": 1440,
    "height": 900
  }
}
```

`start`, `url`, and `health` are required in a manifest. `health` is a relative
path. `url` must use `localhost`, `127.0.0.1`, or `[::1]`. `setup` and
`viewport` are optional. Without a manifest, Multica accepts only one clear
Playwright config, one matching package manager/lockfile, and a `dev:web` or
`dev` script; otherwise preflight asks for the manifest.

The setup hook runs in the normal task sandbox after health succeeds and
receives:

| Environment variable | Purpose |
| --- | --- |
| `MULTICA_UI_TEST_BASE_URL` | Resolved loopback base URL |
| `MULTICA_UI_TEST_STORAGE_STATE` | Task-local path where setup may write browser state |
| `MULTICA_UI_TEST_ARTIFACT_DIR` | Task-local evidence directory |
| `MULTICA_UI_TEST_TASK_ID` | Current task identity |

Use setup to create deterministic test data or authentication state. Never
commit or publish storage state, cookies, tokens, credentials, or headers.
`playwright.config.ts` remains authoritative for persistent regression tests.

## Ordinary issue examples

Assign any of these requests through the normal issue workflow:

| Mode | Example request | Source changes |
| --- | --- | --- |
| audit | Audit login and issue creation for accessibility and UX concerns; attach evidence. | none |
| regression | Add a focused Playwright regression for failed issue creation and run it through the deterministic gate. | focused_playwright |
| both | Audit the settings journey, then add focused coverage for confirmed objective failures. | focused_playwright |

Audit mode must leave tracked source exactly as it began. Regression and
combined modes may change only focused Playwright coverage requested by the
issue and must validate it with `test_gate`.

## Verdicts and findings

`ui_test_report` derives the verdict; agents do not submit one.

| Result | Execution status | Verdict effect |
| --- | --- | --- |
| objective_failure | completed | fail |
| advisory_finding | completed | none |
| infrastructure_error | infrastructure_error | not_run |
| blocked | blocked | not_run |

Explicit assertion failures, relevant first-party uncaught console errors,
first-party request failures that break or corrupt a flow, Axe critical/serious
violations, and nonzero regression gates are objective. Hierarchy, readability,
spacing, copy, discoverability, consistency, feedback, and Axe moderate/minor
findings are advisory. Irrelevant third-party noise is advisory or omitted.

Finding a product failure is a successful investigation with a `fail` verdict.
`not_run` means infrastructure or policy prevented a valid product result.

## Artifacts and publication

Reports live under:

```text
.multica/artifacts/ui-test/<task-id>/report.json
.multica/artifacts/ui-test/<task-id>/report.md
.multica/artifacts/ui-test/<task-id>/artifact-manifest.json
.multica/artifacts/ui-test/<task-id>/comment.md
.multica/artifacts/ui-test/<task-id>/published-evidence/
```

Publish `comment.md` with `--content-file`; attach both reports, the manifest,
and only relevant sealed evidence named by the manifest. If upload fails, local
artifacts remain authoritative and the task reports degraded publication.
Never regenerate or discard the sealed report merely to retry upload.

## Evidence compatibility

| Evidence | V1 report input | Publication |
| --- | --- | --- |
| png | accepted | sealed under `published-evidence/` |
| text_json | accepted | redacted and sealed under `published-evidence/` |
| trace_zip | rejected | do not submit or attach |

V1 intentionally rejects Playwright traces and ZIP files in `ui_test_report`.
Use focused PNG, text, or JSON evidence instead.

## Security limits

- Test only loopback HTTP/HTTPS targets. External top-level navigation,
  redirects, popups, and disallowed requests are blocked.
- Use the managed role/name-based browser tools. Arbitrary page evaluation and
  `browser_run_code_unsafe` are hidden and rejected.
- Use a fresh task-scoped Chromium profile. Never start an ad-hoc browser.
- Capture initial state, meaningful checkpoints, failures, and supported visual
  findings—not every click.
- Retry browser startup only before product interaction. Never replay an
  interacted flow unless the issue explicitly establishes idempotence.
- Keep all source, setup state, logs, and evidence inside the task workspace and
  artifact directory.

## Readiness and troubleshooting

| Symptom or status | Action |
| --- | --- |
| unavailable | Run `multica ui-test install`, then check status again. |
| installing | Wait for the existing pinned installation; do not start another. |
| broken | Read the bounded `error` from `multica ui-test status --output json`, fix the missing/mismatched runtime prerequisite, then reinstall. |
| manifest inference error | Add explicit `.multica/ui-test.json`; do not guess a start command or URL. |
| application or health failure | Fix the local start/health contract; report `infrastructure_error` and `not_run`. |
| setup failure | Check task-local setup logs and required environment; report `infrastructure_error` and `not_run`. |
| browser failure before interaction | Use only the bounded startup retry allowed by managed policy; otherwise report `infrastructure_error`. |
| blocked navigation | Keep the target and every navigation on loopback; do not bypass the proxy. |
| evidence rejected | Remove trace/ZIP or secret-bearing evidence; submit supported PNG/text/JSON within the task run directory. |
| upload failure | Keep local artifacts and report degraded publication; do not regenerate the report. |

Application and browser process groups are daemon-owned and cleaned up on
success, error, timeout, cancellation, or completion.
