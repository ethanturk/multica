-- Backfill the skill_bundle_unavailable reason introduced in MUL-5370.
--
-- The daemon now labels "could not download the agent's skill bundles"
-- structurally instead of letting the wrapped transport error fall through
-- taskfailure.Classify into agent_error.unknown. That change is forward-only:
-- historical rows still sit in the unknown bucket, which makes the failure
-- invisible on the Usage page's Errors breakdown — exactly the observability
-- gap that let this bug survive in the wild.
--
-- The witness is the old wrapper prefix these rows were written with
-- ("resolve skill bundles: ..."), which is stable and unambiguous: no other
-- code path produced it.
--
-- Scoped to agent_error.unknown alone, and that is exhaustive rather than
-- conservative: the wrapper string first shipped 2026-06-23 (MUL-3281,
-- 5038c983c), three weeks after the in-flight classifier landed 2026-06-03
-- (MUL-2946, 10afd1af1). Every row that can carry this prefix was therefore
-- written through taskfailure.Classify and landed in agent_error.unknown --
-- none can hold the pre-MUL-1949 coarse 'agent_error'. Keeping the source
-- bucket single-valued is what makes the down migration an exact inverse.
UPDATE agent_task_queue
SET failure_reason = 'skill_bundle_unavailable'
WHERE status = 'failed'
  AND failure_reason = 'agent_error.unknown'
  AND error LIKE 'resolve skill bundles:%';

-- chat_message mirrors the task's failure_reason for chat turns, and the chat
-- bubble reads it directly — leaving these behind would keep showing the
-- generic "something went wrong" copy on historical turns.
UPDATE chat_message
SET failure_reason = 'skill_bundle_unavailable'
WHERE failure_reason = 'agent_error.unknown'
  AND content LIKE 'resolve skill bundles:%';
