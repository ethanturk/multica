# Coding Handoff Recovery Detection Design

## Problem

`coding_handoff_decide` currently treats any comment containing the target
agent's raw `mention://agent/<id>` URI as an existing handoff. Multica
recognizes Markdown forms `[@Label](mention://agent/<id>)` and
`[Label](mention://agent/<id>)`; brackets and link are mandatory.
Near-misses such as `@Label (mention://agent/<id>)` therefore suppress recovery
even though they never trigger the target agent.

## Design

Change `decorateRecovery` to recognize only a canonical Markdown agent mention
for the exact `next_agent_id`. A malformed near-match will remain on the normal
route, leaving the existing canonical `comment_content` available for the caller
to post again. A valid canonical mention after the latest workflow marker will
continue to add the `_duplicate_or_recovery` route suffix.

Keep the change local to `dettools/coding_handoff_decide.go`. Do not loosen the
server mention parser, add output fields, or change coding-team skill contracts.

## Validation

Add focused tests covering both sides of the boundary:

- Canonical Markdown mention still produces `_duplicate_or_recovery`.
- The reported malformed form does not produce the suffix and the decision
  continues to contain the canonical handoff comment.

Run the focused `server/pkg/detsteps` Go test package, then format and run the
broader relevant Go checks if the focused suite passes.
