# Multica UI/UX Testing Design

**Date:** 2026-07-29

**Status:** Approved design

**Initial scope:** Multica web app, local targets, Chromium

## Summary

Multica will gain a managed UI/UX testing capability that agents can invoke while working on an ordinary issue. The same capability will support:

1. Exploratory UI/UX audits that produce structured findings without changing source code.
2. Persistent regression work that creates or updates repository-owned Playwright tests and validates them through the existing deterministic test gate.

The daemon will provision and manage a pinned Playwright MCP server and Chromium runtime. A thin Multica policy proxy will expose only the browser operations required for UI testing, enforce loopback-only navigation, and reject unsafe operations such as arbitrary Playwright code execution. A built-in skill will teach agents how to plan scenarios, use the managed browser, distinguish objective failures from advisory UX findings, and publish evidence.

V1 deliberately reuses normal issue assignment, task transcripts, the existing Playwright suite, deterministic tools, and issue-comment attachments. It does not introduce a new workflow type, artifact database, or results dashboard.

## Goals

- Let an agent perform UI/UX testing from a normal assigned issue.
- Support both exploratory audits and durable Playwright regression coverage.
- Let Multica dogfood the capability against its own web app.
- Keep browser execution deterministic, isolated, observable, and safe.
- Make objective test failures machine-readable and keep subjective UX judgment advisory.
- Publish concise results with durable evidence on the originating issue.
- Avoid prompted credentials by using repository-owned test setup.
- Leave non-UI agent tasks and existing repository testing behavior unchanged.

## Non-goals

V1 will not include:

- Firefox or WebKit.
- Mobile-device emulation as a default test matrix.
- Electron desktop testing.
- Hosted or remote website testing.
- Visual snapshot baseline management or pixel-diff approval.
- A dedicated UI Test task type or workflow engine.
- A new artifact database or UI testing dashboard.
- Recording and replaying arbitrary user sessions.
- Automatic source-code remediation for subjective UX findings.

## Product Model

UI testing remains part of the existing issue lifecycle:

1. A user describes the desired audit or regression work in a normal issue.
2. The issue is assigned to an agent through the existing task flow.
3. The agent recognizes UI-testing intent using the built-in skill.
4. Multica preflights the repository contract and managed browser capability.
5. The daemon starts the target application and browser session only when needed.
6. The agent executes scenarios and records objective checks and advisory findings.
7. Multica produces structured artifacts and attaches them to a concise issue comment.
8. All managed application and browser processes are cleaned up.

An audit request must not modify repository source. A regression request may create or update Playwright tests and must run the focused tests through the existing `test_gate` path.

The UI under test can receive a failing verdict while the agent task itself completes successfully. Completing the requested investigation and reporting a product failure is successful task execution. Only an inability to execute the requested test is an infrastructure or blocked task outcome.

## Architecture

### Managed Playwright MCP

The daemon will own installation and execution of a pinned Playwright MCP package and compatible Chromium build. Installation is explicit through a command such as:

```text
multica ui-test install
```

The runtime UI will report one of:

- `unavailable`
- `installing`
- `ready`
- `broken`

Multica must not download browsers as a surprise side effect of assigning an issue. Once installed, the capability may be advertised to compatible agent runtimes, but no application or browser process starts until a task invokes UI testing.

Each invocation gets:

- A fresh, isolated Chromium profile.
- Headless execution.
- A task-scoped output directory.
- A fixed output-size limit.
- Loopback-only network access.
- Access only to the active task workspace and evidence directory.

Multica will reuse the official Playwright MCP server for browser mechanics rather than implement a browser automation protocol. A thin policy proxy will sit between the agent runtime and Playwright MCP.

### Policy Proxy

The proxy is the security and policy boundary. It will:

- Filter the MCP tool list to a fixed safe allowlist.
- Reject calls to hidden or unsupported tools even if a client constructs them manually.
- Hide and reject arbitrary code-execution tools, including `browser_run_code_unsafe`.
- Validate top-level navigation, redirects, popups, and relevant subresource origins.
- Permit only `localhost`, `127.0.0.1`, and `[::1]` by default.
- Apply output and timeout limits.
- Normalize browser observations into task transcript events.
- Provide a fixed accessibility scan operation backed internally by Axe.

The safe browser surface should cover:

- Navigate.
- Accessibility snapshot.
- Click.
- Type and fill fields.
- Select options.
- Hover and drag.
- Keyboard input.
- Tab management.
- Viewport resize.
- Screenshot capture.
- Focused console and network inspection.

Generic page evaluation and arbitrary Playwright code are not part of the V1 agent-facing surface. Any future addition must receive an explicit threat review.

### Task Lifecycle Manager

The daemon, rather than the agent shell, owns application and browser process groups. This is required because background agent processes are intentionally rejected by the current runtime.

The lifecycle manager will:

1. Resolve the repository UI-testing configuration.
2. Start the configured application command in a managed process group.
3. Poll the configured health endpoint.
4. Run the optional authentication/setup hook.
5. Launch an isolated browser through the policy proxy.
6. Provide the safe MCP connection to the agent runtime.
7. Retain task-local logs and evidence.
8. Terminate all child processes on completion, error, timeout, or cancellation.

Startup, health checking, setup, navigation, and total execution each require bounded timeouts.

### Built-in Skill

A built-in `multica-ui-testing` skill will define the agent operating procedure:

- Determine whether the issue requests an audit, regression coverage, or both.
- Identify the minimum important user journeys.
- Prefer accessible roles and names over brittle selectors.
- Record explicit objective expectations before interaction.
- Capture evidence at meaningful checkpoints and failures.
- Classify objective checks separately from advisory UX findings.
- Avoid repository edits during audit-only work.
- Use the existing Playwright configuration for persistent regression tests.
- Run regression tests through `test_gate`.
- Publish the deterministic report and issue comment.

The skill guides behavior; it does not replace daemon enforcement.

## Repository Contract

Repositories may add an optional `.multica/ui-test.json`:

```json
{
  "start": "make start",
  "url": "http://127.0.0.1:3000",
  "health": "/login",
  "setup": "pnpm ui-test:setup"
}
```

Fields:

- `start`: command the daemon runs as the managed application process.
- `url`: loopback base URL for the test session.
- `health`: path polled before browser work begins.
- `setup`: optional command that creates test data and/or authentication state.

The manifest is optional. When absent, Multica may infer conservative defaults from repository configuration. Ambiguous inference must produce a clear preflight error rather than selecting an unsafe or surprising command.

The setup hook:

- Runs in the normal task sandbox.
- May create a test user, deterministic test data, or Playwright `storageState`.
- Writes generated state only to a task-local directory.
- Must not commit credentials, tokens, cookies, or generated state.
- Receives no special unrestricted secret access.
- Returns the location of its generated authentication state through a defined task-local contract.

An existing repository `playwright.config` remains the source of truth for persistent regression tests. The managed browser capability does not create a competing test configuration.

## Testing Method

### Default Environment

V1 uses:

- Chromium.
- Headless mode.
- A default viewport of 1440 by 900.
- A clean browser profile for each task.
- The repository-defined local application.

The issue or repository manifest may request a different desktop viewport. Mobile matrices are deferred.

### Objective Checks

Objective failures affect the verdict:

- A required user flow or explicit assertion fails.
- An uncaught page error occurs.
- An unexpected first-party console error relevant to the scenario occurs.
- An unexpected first-party request failure prevents or corrupts the flow.
- Axe reports a critical or serious accessibility violation.
- A persistent Playwright regression test exits nonzero through `test_gate`.

Third-party noise must not automatically fail a scenario. The report must identify why a console or network event is first-party and relevant before treating it as objective.

### Advisory Findings

The following are advisory and do not affect the verdict:

- Visual hierarchy.
- Readability and information density.
- Spacing and alignment.
- Copy clarity.
- Discoverability and affordance.
- Consistency.
- Feedback and perceived responsiveness.
- Moderate or minor accessibility concerns.
- Irrelevant third-party console or network noise.

Each advisory finding must include a clear observation, user impact, evidence, and a suggested direction. It must not be described as an objective test failure.

### Evidence

The agent captures:

- The initial state.
- Important flow checkpoints.
- Every objective failure.
- Every advisory finding that depends on visual evidence.

It should not capture a screenshot after every click.

Evidence types include:

- Accessibility snapshots for structure, roles, names, and semantics.
- PNG screenshots for visual state.
- Focused console or network excerpts.
- Playwright traces for persistent regression failures or when explicitly requested and supported by the validated runtime configuration.

All evidence is first written to the task-local output directory.

## Deterministic Reporting

A deterministic `ui_test_report` tool will validate the submitted result structure, derive counts, and compute the final verdict from objective-check statuses. The agent does not directly choose the final `pass` or `fail` value.

The core report shape is:

```json
{
  "schema_version": "1",
  "execution_status": "completed",
  "verdict": "pass",
  "target": {
    "url": "http://127.0.0.1:3000",
    "commit": "..."
  },
  "environment": {
    "browser": "chromium",
    "viewport": {
      "width": 1440,
      "height": 900
    }
  },
  "scenarios": [],
  "objective_checks": [],
  "advisory_findings": [],
  "artifacts": []
}
```

`execution_status` is one of:

- `completed`
- `infrastructure_error`
- `blocked`

`verdict` is one of:

- `pass`: execution completed and all objective checks passed.
- `fail`: execution completed and at least one objective check failed.
- `not_run`: execution could not reach a valid test result.

The invariant is:

```text
execution_status != completed  => verdict = not_run
any objective check failed     => verdict = fail
otherwise                      => verdict = pass
```

The reporting tool emits:

- Validated JSON.
- Markdown derived from the same data.
- An artifact manifest.
- A concise issue-comment body.

The issue comment will include:

- Verdict and execution status.
- Target and tested commit.
- Scenario and objective-check counts.
- A compact list of objective failures.
- Advisory finding count.
- Links to attached JSON, Markdown, screenshots, and other evidence.

V1 will use existing task-scoped attachment upload and issue-comment attachment support. A new durable artifact table is unnecessary.

If attachment upload fails, the report remains in the task output directory and the task reports degraded publication rather than silently discarding the result.

## Failure Handling

| Condition | Classification |
| --- | --- |
| UI-testing capability is not installed | `blocked`, with installation guidance |
| Application fails to start | `infrastructure_error` |
| Health check times out | `infrastructure_error` |
| Authentication/setup hook fails | `infrastructure_error` |
| Browser fails before useful testing | `infrastructure_error` |
| Product assertion or objective check fails | `completed` with verdict `fail` |
| Advisory concern is found | `completed`; verdict unchanged |
| Evidence publication fails | Preserve local report and mark publication degraded |

Multica will not automatically replay a flow after user interactions have begun. Replaying may duplicate destructive actions or create misleading evidence. Browser startup may be retried only before any product interaction and only under a tightly bounded policy.

## Security

- Default-deny all non-loopback origins.
- Validate URLs at every navigation boundary, not only the initial target.
- Use a fresh browser profile per task.
- Do not expose arbitrary code-execution browser tools.
- Apply the allowlist both when listing and invoking MCP tools.
- Restrict filesystem access to the task workspace and evidence directory.
- Enforce log, report, screenshot, and total artifact size limits.
- Redact known secrets from logs and text artifacts before publication.
- Never publish browser storage state, cookies, authorization headers, or credentials.
- Terminate application and browser process groups on every exit path.
- Keep provisioning explicit and version-pinned.

## Delivery Slices

### Phase 1: Safe Browser Foundation

- Pinned Playwright MCP and Chromium provisioner.
- Runtime readiness status and installation command.
- Policy proxy and loopback enforcement.
- Per-task application/browser lifecycle manager.
- Optional manifest parser.
- Built-in UI-testing skill.
- Deterministic UI report tool.
- Existing issue-attachment publication.

### Phase 2: Multica Dogfooding

- Repository-owned test-user and authentication setup.
- Task-local Playwright storage-state generation.
- Initial exploratory scenarios for login, navigation, issues, and settings.
- Accessibility checks for the same scenarios.
- Existing Playwright suite connected to deterministic reporting.
- Verification that audit mode leaves the source tree unchanged.

### Phase 3: Regression Authoring

- Agent guidance for creating and updating focused Playwright tests.
- Focused regression execution through `test_gate`.
- Failure screenshots, logs, and traces attached when available.
- Report presentation improvements informed by dogfood results.

## Acceptance Criteria

V1 is accepted when:

- An ordinary assigned issue can invoke UI testing.
- No dedicated workflow or task type is required.
- Installation is explicit and runtime readiness is visible.
- Browser sessions are isolated and Chromium-only.
- Unsafe browser tools are absent and rejected when called directly.
- External navigation, redirects, popups, and disallowed requests are blocked.
- Cancellation, timeout, failure, and success all clean up managed processes.
- Objective product failures produce `completed` reports with verdict `fail`.
- Infrastructure failures produce verdict `not_run`.
- Advisory findings never alter the verdict.
- Audit-only execution leaves repository source unchanged.
- Regression work can create a Playwright test and validate it through `test_gate`.
- JSON, Markdown, and evidence attach to the originating issue comment.
- Multica can test its own web app without prompting for credentials.
- Existing non-UI agent tasks behave as before.

## Validation Strategy

The implementation will require:

- Unit tests for manifest parsing, URL/origin policy, tool filtering, verdict computation, redaction, and error classification.
- Integration tests using a local fixture application for allowed navigation, blocked external navigation, redirects, popups, console errors, request failures, accessibility violations, screenshots, cancellation, and cleanup.
- Runtime tests proving hidden MCP tools cannot be invoked directly.
- Lifecycle tests proving application and browser process groups do not survive terminal task states.
- Report tests proving JSON, Markdown, counts, verdicts, and attachment references remain consistent.
- Repository dogfood tests proving authenticated Multica flows work with task-local setup state.
- Regression tests proving the existing Playwright suite remains the configuration authority.
- Non-regression tests proving ordinary agent tasks do not provision or launch a browser.

## Key Decisions

1. Use managed Playwright MCP rather than a custom browser automation plane.
2. Put a Multica policy proxy in front of Playwright MCP.
3. Trigger UI testing from ordinary issues.
4. Support exploratory audits and persistent Playwright regressions.
5. Keep audit findings advisory unless an objective check fails.
6. Treat tested-product failures separately from task infrastructure failures.
7. Use repository-owned setup hooks rather than prompted credentials.
8. Reuse existing attachments instead of adding an artifact database.
9. Start with local web targets and Chromium.
10. Dogfood the feature on Multica before expanding scope.
