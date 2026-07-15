---
name: agent-improvement-loop
description: Stage 1-8 agent improvement loop — nightly telemetry capture, pain-bucket analysis, candidate dettool generation, replay evaluation, and promotion to production.
---

# Agent Improvement Loop (AIL)

The AIL is a self-improving feedback loop for Multica. Each night it pulls recent agent telemetry, surfaces recurring pain patterns, generates candidate deterministic tools (dettools) to address them, and — when a candidate earns human approval and survives replay evaluation — promotes it into the production dettool catalog.

The loop is split across three sub-skills. Load only the one you need for the current stage.

## Stages at a glance

| Stage | Sub-skill | What it does |
|---|---|---|
| 2 + 3 | `analyzer` | Capture and index telemetry events; produce pain-bucket digests with repeat signatures and candidate suggestions |
| 4 | `evaluator` | Evaluate candidate dettools against deterministic subsets of historical events |
| 5 | `evaluator` | Consume Stage 4 decisions and route each recommendation to approval, review, or defer |
| 6 | `evaluator` | Generate the approved candidate scaffold (source + test + manifest entry) |
| 7 | `evaluator` | Replay-based evaluation with strict determinism profile and rerun filters |
| 8 | `evaluator` | Promote the winning candidate into production via the transactional helper |

## Files in this skill

- `SETUP.md` — install the two dedicated agents and the nightly autopilot schedule. Read once during initial setup.
- `analyzer.md` — load this skill when operating Stage 2 + Stage 3 (the nightly pass).
- `evaluator.md` — load this skill when operating Stage 4, 6, 7, or 8 (candidate governance and promotion).

## Quick start

1. **One-time setup**: read `SETUP.md`, create the `Agent Improvement Analyzer` and `Agent Improvement Evaluator` agents, and wire the Stage 2+3 nightly autopilot at `0 2 * * *` UTC.
2. **Nightly (Stage 2-3)**: the analyzer agent runs `multica ail run` and posts a digest comment on the tuning issue.
3. **Immediate handoff (Stage 3 -> Stage 4)**: after Stage 3 completes, load `diagnostics/stage3/stage3_digest.json` and `diagnostics/stage3/stage3_signatures.jsonl`, then call the default-allowlisted `agent_improvement_evaluate` dettool with the Stage 3 `candidate_dettools` and `repeat_signatures` payloads. There is no `multica ail stage4` CLI command in this workflow.
4. **On demand (Stage 4-8)**: the evaluator consumes the Stage 4 decisions. `ready_for_candidate` flows into Stage 6 after human approval, `ready_for_review` is posted for human review, and `defer` stops without scaffolding.

## Required deterministic tools

- `agent_improvement_evaluate` (Stage 4 evaluator — set up by the build, must be imported per workspace)
- `pipeline_state_parse`, `repo_facts`, `diff_summarize` (context helpers)

## Promotion rule (canonical sequence)

Per `SETUP.md`, every Stage 8 promotion must:

1. Move candidate source `dettools/prospect/*.go` → `dettools/*.go`
2. Run `multica dettool import-file dettools/<tool>.go`
3. Refresh `skills/agent-improvement-loop/*` if the new tool belongs in required imports
4. Append an immutable entry to `diagnostics/stage8-promotion.jsonl`

Or use the transactional helper:

```bash
scripts/stage8-promote.sh --tool <tool_name> --approve-ref <issue-or-pr> [--force] [--skip-import] [--dry-run]
```

## Prospect vs. production

- `dettools/*.go` — production catalog, importable into agents
- `dettools/prospect/*.go` — candidate stage, **never import** until Stage 8 promotion completes
- `dettools/prospect/manifest.json` — machine-readable tracking of candidate lifecycle (`draft` → `awaiting_human_approve` → `candidate` → `rejected`/`promoted`/`archived`)
