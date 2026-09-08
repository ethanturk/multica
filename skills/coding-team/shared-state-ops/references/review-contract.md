# Shared delivery and review contract

Read before planning, implementation, testing, or review. Role-specific boundaries, pause checks, and dispatch guards remain mandatory. Do not load unrelated operational references.

## Identity and scope

Use live task/master JSON and validated artifacts: exact task UUID, master UUID, canonical branch, task-start SHA, plan/criteria revision, submitted SHA, and cumulative task files. Reconcile child criteria, master amendments, and plan before work; scope disagreement requires Orchestrator/Planner, not a silent expansion or stale subset. Never normalize identifiers or create branch aliases. Reconstruct missing baselines from evidence, never guessed SHAs.

Sort complete comments by creation time and ID before `coding_comment_extract`; validate artifact IDs and distinguish actual lifecycle artifacts from quoted headings. Verify commit ancestry and intervening diffs, not SHA equality between implementation and descendant test commits. Separate sibling tasks on the shared branch. Post artifacts and work-bearing mentions on the exact task; verify readback before dispatch.

## Acceptance and verification

Planner records in `key_decisions` and `acceptance_criteria_coverage`: guidance/ADR paths, entry points, observable invariants, negative/fault scenarios, verification commands and prerequisites, cumulative production-file denominator, non-goals, sibling ownership, and applicable manual style checks. No new artifact type is required.

Read applicable AGENTS/CLAUDE/STYLE guidance, including referenced paths and app-specific rules. Repository conventions win over generic preferences. Verify behavior, production wiring, real internal collaborator contracts, test semantics, formatter/lint/typecheck, coverage, and mandatory manual style. Exercise affected sibling call paths, ordering, cancellation, cleanup, privacy, and fail-closed behavior. Mock external I/O, not the internal contract under test. Keep live authenticated checks outside isolated unit tests. Record checkout/cwd, candidate SHA, exact commands, results, and unverified areas. Coverage never substitutes for behavior.

## Coverage

Require at least 99% line coverage per applicable changed production file across the cumulative task, not the latest repair, new module alone, or package/assembly average. Inspect per-file reports; aggregate exit success is insufficient. Missing/unmeasured production files cannot be silently excluded.

For documentation/test-only tasks with no applicable production files, coverage is N/A. Execute planned reproducible semantic checks and impacted tests, record the empty denominator, omit unmeasured numeric percentages, and explain N/A in artifact coverage details. `passed` describes applicable validation, not an invented measurement.

For Python, use the repository's pytest/coverage commands with term-missing output and a 99% threshold. For C#, use `dotnet_test_gate` through MCP, not a direct dotnet command: `targets`, `collect_coverage: true`, `coverage_threshold: 99`, and required Include filters in `msbuild_properties`. Inspect per-file results. Executed test/coverage failures are findings; unavailable SDKs, credentials, tools, wrong roots, or malformed artifacts are blocked verification. Add tests for uncovered behavior; justify each exclusion individually rather than gaming coverage.

## Findings and repair

Complete the scoped first review and post one consolidated verdict. Give blockers stable IDs in existing finding-message fields, each with criterion/mandatory rule, file/line, evidence, impact, minimal fix, and closure check. Group instances of one defect. Optional style preferences, speculative cleanup, and unrelated defects are non-blocking.

Implementer fixes production and tests, maps every finding ID to changes and executed regression evidence, and returns to Reviewer. Initial independent testing is mandatory. Later independent Test Writer work requires the explicit `requires_test_writer: true` line and outstanding scope; ordinary regression-test requests do not require it. Reviewer uses prior independent artifacts plus fresh candidate-specific repair evidence and independently verifies the candidate. The extractor's `tests_needed` hint does not override this rule.

Retain IDs across repairs. Explain whether new material blockers were introduced or previously missed. After two failed completed repair attempts across review/refinement, or unchanged repeated findings without new evidence, preserve blockers and post `## Review Blocked: Decision Needed`. Notify Orchestrator once; keep the task open, stop automatic dispatch, and require an explicit resolving decision defining the next bounded attempt. Prerequisite/scope/artifact blockers take this same path, never fabricated PASS or a code FAIL. Duplicate triggers must not replay completed work or dispatch to active/already-delivered successors.
