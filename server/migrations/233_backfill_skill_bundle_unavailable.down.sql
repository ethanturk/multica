-- Reverse the backfill: return the re-classified rows to agent_error.unknown,
-- the single bucket the up migration took them from. Restricting the filter to
-- the old wrapper prefix keeps the inverse exact — rows this migration never
-- touched (a new daemon writes skill_bundle_unavailable directly, with a
-- "skill bundle unavailable: ..." error string) are left alone rather than
-- being relabelled into a bucket they were never in.
UPDATE agent_task_queue
SET failure_reason = 'agent_error.unknown'
WHERE status = 'failed'
  AND failure_reason = 'skill_bundle_unavailable'
  AND error LIKE 'resolve skill bundles:%';

UPDATE chat_message
SET failure_reason = 'agent_error.unknown'
WHERE failure_reason = 'skill_bundle_unavailable'
  AND content LIKE 'resolve skill bundles:%';
