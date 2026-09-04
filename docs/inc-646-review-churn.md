# INC-646 review-churn analysis

Source: https://multica.apps.ethanturk.com/incycle/issues/INC-646

## Scope and evidence

Read using the authenticated Multica CLI against the InCycle workspace. The snapshot includes the master issue, every child recorded in its pipeline state, and all their comments. Paginated workspace discovery independently confirmed the same five descendants and no additional issue descriptions referencing this master UUID. Full comment counts were checked against each issue's root comments plus declared descendant reply counts.

| Issue | Comments | Review FAIL headings | Review PASS headings | Refinement FAIL headings |
| --- | ---: | ---: | ---: | ---: |
| INC-646 | 95 | 0 | 0 | 0 |
| INC-647 | 38 | 3 | 1 | 0 |
| INC-648 | 59 | 10 | 2 | 1 |
| INC-649 | 33 | 7 | 1 | 3 |
| INC-650 | 12 | 0 | 0 | 0 |
| INC-651 | 81 | 16 | 0 | 0 |

Total: 318 comments, 36 Review FAIL headings, and 4 Refinement FAIL headings. These are posted verdict-heading counts, not unique defects, completed executions, or measured wasted runs. The master also contains the guided-planning interview, so its comment volume is not review churn. This is a snapshot of active work, not a claim that the pipeline is complete.

The seven workspace role skills matched their repository originals before this change. This was not simply a stale workspace-copy problem.

## What caused avoidable churn

### 1. Repair routing and artifact requirements contradicted each other

INC-651 repeatedly alternated substantive review with a rejection demanding a new independent Test Writer artifact after an Implementer repair. Examples:

- `01a06cda-09f5-7b4d-a850-eccabd7e4df0`
- `01a06dc1-22f0-76b3-b5da-52b5f36dab18`
- `01a06dd9-969f-7d6b-98ae-129fa7d000bb`
- `01a06df0-b2d5-7391-896a-e44949cbf4ff`
- `01a06e04-8af9-7baa-bc4b-65884ec9da4e`

Those comments demand another independent test handoff despite the Implementer skill's direct review-fix route. Other artifact failures also had legitimate missing implementation evidence or insufficient coverage; they must not all be counted as unnecessary.

Change: retain initial independent testing; require fresh candidate-specific repair verification, but not a new independent Test Writer artifact on every repair. Only the explicit `requires_test_writer: true` marker requests that extra stage. Validate commit ancestry and intervening changes instead of requiring implementation and test commits to be identical. The existing extractor's `tests_needed` flag is not the routing authority.

### 2. Routing and scope evidence were unstable

INC-647's original plan was posted on the master (`01a05a07-34b4-7960-89c8-4a7d38d73f71`), then Test Writer reported it missing on the task (`01a05a22-2fd1-7cac-98ea-d70372ab0a0f`). Its completion later triggered planning for INC-649 on the completed INC-647 thread (`01a05dda-f64e-77dc-89a1-f63e95c9e293`, `01a05de5-24c7-7ab3-b0ef-6d19dac9085a`). A link to another task did not move the artifact there.

A branch mismatch complaint (`01a05a28-1279-7c0a-88d2-54602f7065e1`) was repeated, then Implementer reported that all inspected metadata used the canonical dash spelling and nevertheless created an underscore mirror (`01a05d34-c468-7555-b0c7-105a7ac03abf`). This added ongoing mirror-maintenance work rather than resolving the unsupported source of the mismatch.

INC-648's first FAIL (`01a05ea7-579a-7e78-8634-3bd181d3f35f`) also found that implementation/test artifacts covered the child's stale four criteria while the master carried eight amended criteria.

Change: verify exact task/branch/artifact identity before work or dispatch, reconcile child/master/plan criteria before coding, post work-bearing mentions on the actual task UUID, and define a cumulative task scope distinct from the entire shared feature branch. New files listed under `files_to_create` are expected to be absent; the old Implementer rule incorrectly treated that as a Planner blocker.

### 3. Repairs did not consistently exercise real integration boundaries

These were substantive defects, not merely an overstrict reviewer:

- INC-648 `01a06807-1e2b-7d35-88a1-6f4f1a3829f1`: the recorder passed bytes to a real store expecting a typed model; a permissive fake hid the incompatibility.
- INC-648 `01a06864-979e-7b0f-bf5e-cd4b5fe95909`: Refiner reproduced request-local preflight stored on a singleton and overwritten by another request.
- INC-649 `01a06b70-22a0-71d7-8720-6ff1d5cf1341`: a production issuer stripped the helper grant even though a fabricated test delegation included it.
- INC-651 `01a06cbb-93f3-7d54-bc7c-fe2e58d68bb8`: real DI did not provide the terminal store; release-proof IDs differed from persisted call IDs; result buffering was not forwarded along the production path.
- INC-651 `01a06dd2-920d-7716-aefe-6d5adf002411`, `01a06de8-4119-7979-ac76-3f8425527ccf`, and `01a06e10-27a5-78fb-87f4-4b3788f7ab90`: real SDK probes exposed late charging, parser bypasses, and wrong-response-ID selection despite green aggregate gates.

Change: Planner traces entry points and dependencies without an arbitrary eight-file ceiling. Implementer/Test Writer validate real internal collaborators, externally mocked boundaries, negative paths, ordering, cleanup, privacy, and integration wiring. Every repair maps the entire finding list to changes and executed regression evidence. Coverage is not a substitute for behavior.

### 4. Style and verification rules were applied late or inconsistently

INC-647 received semantic corrections first (`01a05d43-353b-76cc-9af0-1e7bd8c8f32e`), test isolation/structure/lint corrections next (`01a05d8a-f444-724e-87fa-8b1d8092003a`), and then a style-only FAIL (`01a05dae-d68a-7877-a3aa-22198511212b`). New repair code can introduce new defects, but a shared checklist would have caught helper comments and AAA conventions before handoff. The existing Implementer and Test Writer skills even prescribed different test-name formats.

INC-651's final captured FAIL distinguished 99.12% aggregate changed-production coverage from a modified module's 97.91% coverage. The old skills mixed per-file language with aggregate coverage-command examples.

Change: all roles use the discovered repository guidance and the same automated/manual checklist. Reviewer completes the full scoped first pass, retains stable finding IDs, and explains newly introduced or previously missed blockers. Per-file coverage reports must substantiate the existing 99% requirement; scope cannot shrink to just the newest module or repair. Documentation-only tasks use semantic validation with runtime coverage N/A, and live authenticated checks stay outside isolated tests.

### 5. Recovery could recreate work that had already succeeded

INC-647 contains repeated harness failures after durable delivery. Recovery comments such as `01a05d79-7356-7b9f-96dc-532f12e62b54` and `01a05da4-5362-73ea-acc6-0c5a4c136866` correctly avoided reassigning work because successor runs were already active. The old role idempotency instructions nevertheless said to re-emit handoffs unconditionally.

Change: check successor executions and later valid artifacts before replaying a handoff. Do not repost completion artifacts on duplicate triggers. Distinguish an environment/artifact/scope blocker from a code FAIL. After two failed completed repair attempts, or repeated unchanged findings without new evidence, retain the blockers and pause for an explicit decision. Orchestrator and Watchdog must respect that pause; Refiner does not get a fresh unlimited repair budget.

## Changed skill surfaces

- `skills/coding-team/planner.md`
- `skills/coding-team/implementer.md`
- `skills/coding-team/test-writer.md`
- `skills/coding-team/reviewer.md`
- `skills/coding-team/refiner.md`
- `skills/coding-team/orchestrator.md`
- `skills/coding-team/watchdog.md`
- `skills/coding-team/SETUP.md` documents the coordinated rollout and limitations.

No product source, issue status, issue comment, branch, commit, or agent assignment is changed by this skill-maintenance work. Existing user edits outside these scoped changes are preserved.

## Follow-up review and rollout

The follow-up skill review's actionable contradictions were resolved:

- Unsupported refinement-only repair routing is blocked before applying deterministic state patches or dispatching Test Writer.
- Refiner checks unresolved pauses before idempotency and every replay, and uses successor evidence before recovering either handoff.
- Refinement blockers explicitly reopen tasks previously closed by Reviewer; successful refinement closes and verifies them before `TASK_COMPLETE`.
- Implementer's critical rules now honor duplicate/no-op, pause, recovery, and explicit Test Writer exceptions.
- Implementer and Test Writer share the documentation/test-only N/A coverage path, with executed semantic validation and no fabricated coverage percentage.

The seven role skills are synchronized through the Multica CLI to workspace `c8656817-a172-4f7a-9e48-c594015ec27d` (InCycle). Readback verification compares each full skill body with its local source. Names, configuration, and existing bindings are preserved; no issue work is redispatched as part of this rollout.

## Skill slimming and verified deployment

Removed duplicate Implementer/Test Writer commit helpers in favor of the existing shared procedure; replaced embedded Python/C# style lists with applicable repository guidance; consolidated the delivery/review contract into one required shared reference; compressed Orchestrator's repeated prohibitions; and moved six configuration/stage procedures into selectively loaded references. Reviewer and Refiner remain separate roles. Pause, task identity, duplicate suppression, and unsupported-routing guards remain in entrypoints.

Measured main-body character counts (before this slimming pass versus after):

| Role | Before | After | Reduction |
| --- | ---: | ---: | ---: |
| planner | 21,787 | 20,294 | 6.9% |
| implementer | 27,310 | 12,503 | 54.2% |
| test-writer | 16,829 | 9,734 | 42.2% |
| reviewer | 14,894 | 9,649 | 35.2% |
| refiner | 9,338 | 8,555 | 8.4% |
| orchestrator | 29,175 | 7,468 | 74.4% |
| watchdog | 10,422 | 10,422 | 0.0% |

Combined seven-role entrypoints: 129,755 → 78,625 characters, 39.4% smaller. These counts exclude supporting files and are not measured prompt/billing savings. The shared contract still needs to be read for delivery work; stage references are loaded only when relevant.

CLI synchronization verified exact content for eight skill bodies (seven roles plus shared-state-ops) and twelve attached references. Seven references were newly uploaded; five existing shared references already matched. All seven role agents have shared-state-ops enabled. Unrelated remote files, configuration, and names were preserved.

Validation passed: five structural Python unit tests, `git diff --check`, targeted Go `TestCoding` tests, and targeted daemon skill-file/visibility tests. These verify packaging and structural guard retention, not future agent behavior or achieved churn reduction. Local edits remain uncommitted.

## Limits and evaluation

These changes improve instructions, not the deterministic state machine. The current tools use substring markers and positional ordering; they do not enforce candidate ancestry, decision-needed pauses, or the repair budget. Their proposed routing must remain subject to the skill guards. A subsequent code change should make those checks explicit and replay-test them against sanitized histories.

A lower FAIL count alone would not prove success. Compare future similar tasks using substantive review rounds, artifact-only rejections, duplicate dispatches, repeated finding IDs, and elapsed time to accepted work. Keep escaped defects and security regressions as counter-metrics. No reduction percentage is claimed before observing new runs.
