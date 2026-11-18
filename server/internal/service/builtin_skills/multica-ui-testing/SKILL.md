---
name: multica-ui-testing
description: Use when an ordinary Multica issue requests a web UI audit, UX review, accessibility check, browser interaction, or Playwright regression coverage.
user-invocable: false
---

# Multica UI testing

Test local web UI through the managed Chromium capability. Keep product
failures separate from test infrastructure failures. Keep subjective UX
judgment advisory.

## Required tools

| Role | Name | Input rule |
| --- | --- | --- |
| managed_browser | `multica-ui-test` | safe_tools_only |
| reporter | `ui_test_report` | omit_verdict |
| baseline | `repo_facts,diff_summarize` | before_and_after |
| regression_gate | `test_gate` | persistent_playwright_changes |

## Modes

| Mode | Tracked source | Persistent tests | Required gate |
| --- | --- | --- | --- |
| audit | forbidden | none | `diff_summarize` |
| regression | focused_playwright_only | required | `test_gate` |
| both | focused_playwright_only | required | `test_gate` |

For `both`, finish the audit observations before editing focused Playwright
tests. Never remediate advisory findings unless the issue separately requests
implementation.

## Workflow

1. `classify_request` — Classify the request as `audit`, `regression`, or `both`.
2. `record_baseline` — Call `repo_facts` and `diff_summarize`; retain the tracked-file baseline.
3. `preflight` — Confirm `multica-ui-test` is available and the repository resolves `.multica/ui-test.json` or safe inferred configuration.
4. `declare_journeys` — Declare the minimum user journeys and explicit objective expectations before interaction.
5. `navigate_safely` — Use accessible roles and names with managed browser tools against the configured loopback target.
6. `capture_evidence` — Capture initial state, meaningful checkpoints, failures, and visually supported findings. Capture focused console/network excerpts and state why each failing excerpt is first-party and relevant. Do not screenshot every click.
7. `scan_accessibility` — Run `browser_accessibility_scan` at stable page states.
8. `classify_results` — Apply the classification table. Keep objective checks and advisory findings separate.
9. `write_regression` — Only in `regression` or `both`, create or update focused repository-owned Playwright tests and run them through `test_gate`.
10. `seal_report` — Call `ui_test_report` with execution, scenarios, checks, findings, and supported evidence. Do not supply `verdict`; use the returned generated paths and derived verdict.
11. `publish` — Publish the generated comment and required attachments. Attach only relevant sealed evidence listed under `published-evidence/` in the artifact manifest.
12. `verify_audit` — In `audit`, call `repo_facts` and `diff_summarize` again and prove tracked source matches the baseline.

## Classification

| Observation | Channel | Status |
| --- | --- | --- |
| assertion_failed | objective | failed |
| relevant_first_party_uncaught_console_error | objective | failed |
| relevant_first_party_request_failure_breaks_or_corrupts_flow | objective | failed |
| axe_critical_serious | objective | failed |
| regression_test_gate_nonzero | objective | failed |
| ux_hierarchy_copy_spacing_discoverability | advisory | record |
| axe_moderate_minor | advisory | record |
| irrelevant_third_party_noise | advisory_or_omit | record_or_omit |
| infrastructure_failure | execution | not_run |

Treat a network failure as objective only when it prevents or corrupts the
tested flow. An application start, health, setup, authentication, or pre-use
browser failure is infrastructure: set `execution_status` to
`infrastructure_error` or `blocked`; the reporter derives `not_run`.

## Execution outcomes

Choose the execution outcome before evaluating objective-check status. For
`infrastructure_error` and `blocked`, ignore earlier objective failures when
selecting the verdict.

| Case | Execution status | Objective failures | Verdict |
| --- | --- | --- | --- |
| completed_failed | completed | one_or_more | fail |
| completed_passed | completed | none | pass |
| infrastructure_error | infrastructure_error | ignored | not_run |
| blocked | blocked | ignored | not_run |

## Safety and retries

| Situation | Action | Condition |
| --- | --- | --- |
| external_navigation | prohibit | loopback_only_including_redirects_popups_subresources |
| arbitrary_page_evaluation | prohibit | use_fixed_accessibility_scan |
| browser_startup_before_interaction | bounded_retry | capability_policy_allows |
| flow_after_product_interaction | no_retry | unless_issue_explicitly_idempotent |

Never invoke hidden `browser_evaluate`, `browser_run_code_unsafe`, ad-hoc
browser processes, or external URLs. Never publish storage state, cookies,
authorization headers, credentials, or tokens.

## Publication

| Outcome | Task response | Regenerate report | Local artifacts |
| --- | --- | --- | --- |
| success | published | never | retain |
| upload_failure | degraded | never | retain |

Use the paths returned by `ui_test_report`:

```bash
ISSUE_ID="<issue id from task context>"
multica issue comment add "$ISSUE_ID" \
  --content-file .multica/artifacts/ui-test/"$MULTICA_TASK_ID"/comment.md \
  --attachment .multica/artifacts/ui-test/"$MULTICA_TASK_ID"/report.json \
  --attachment .multica/artifacts/ui-test/"$MULTICA_TASK_ID"/report.md \
  --attachment .multica/artifacts/ui-test/"$MULTICA_TASK_ID"/artifact-manifest.json
```

If upload fails, preserve every local artifact, report degraded publication in
the task response, and do not regenerate or discard the sealed report.

## Publication attachments

| Artifact | Flag | Policy |
| --- | --- | --- |
| comment.md | `--content-file` | required |
| report.json | `--attachment` | required |
| report.md | `--attachment` | required |
| artifact-manifest.json | `--attachment` | required |
| sealed_png_text_json | `--attachment` | relevant_only |
| trace_zip | none | rejected_v1 |

V1 `ui_test_report` accepts PNG screenshots and supported text/JSON evidence.
It rejects Playwright trace/ZIP evidence; do not submit or attach traces.

## Source map

Read [the UI-testing source map](references/ui-testing-source-map.md) when
validating commands, security boundaries, report behavior, or repository
configuration.
