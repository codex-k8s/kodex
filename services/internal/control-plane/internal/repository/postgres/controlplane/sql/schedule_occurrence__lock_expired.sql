-- name: ScheduleOccurrenceExpiredCandidates
SELECT
    occurrence.id::text,
    occurrence.schedule_id::text,
    occurrence.organization_id::text,
    occurrence.project_id::text,
    occurrence.scheduled_for,
    occurrence.target_resource_id::text,
    occurrence.target_kind,
    occurrence.target_version,
    occurrence.effective_input_sha256,
    occurrence.prompt_profile_id::text,
    occurrence.prompt_revision,
    occurrence.runtime_revision_id::text,
    occurrence.session_policy,
    coalesce(occurrence.room_id::text, ''),
    occurrence.notification_policy,
    occurrence.maximum_execution_duration_ms,
    occurrence.coalesce,
    occurrence.overlap_policy,
    occurrence.maximum_attempts,
    occurrence.initial_backoff_ms,
    occurrence.maximum_backoff_ms,
    occurrence.dead_letter_at,
    occurrence.state,
    occurrence.version,
    occurrence.attempt,
    coalesce(occurrence.claimant_workload_id, ''),
    coalesce(occurrence.authority_generation, 0),
    coalesce(occurrence.token_hash, ''),
    coalesce(occurrence.claim_key_sha256, ''),
    coalesce(occurrence.lease_expires_at, 'epoch'::timestamptz),
    occurrence.available_at,
    coalesce(occurrence.outcome, ''),
    coalesce(occurrence.result_artifact_id::text, ''),
    coalesce(occurrence.recovery_evidence_sha256, ''),
    coalesce(occurrence.recovery_blocked_at, 'epoch'::timestamptz),
    coalesce(occurrence.execution_session_id::text, ''),
    coalesce(occurrence.execution_session_version, 0),
    coalesce(occurrence.execution_turn_id::text, ''),
    coalesce(occurrence.execution_turn_version, 0),
    coalesce(occurrence.execution_process_run_id::text, ''),
    coalesce(occurrence.execution_process_version, 0),
    coalesce(occurrence.execution_runtime_revision_id::text, ''),
    coalesce(occurrence.execution_runtime_revision_version, 0),
    occurrence.claimed_at,
    occurrence.updated_at
FROM control_plane.schedule_occurrences AS occurrence
WHERE occurrence.organization_id = @organization_id::uuid
  AND occurrence.project_id = @project_id::uuid
  AND occurrence.state IN ('RESERVED', 'CLAIMED')
  AND occurrence.lease_expires_at <= @now
ORDER BY occurrence.lease_expires_at, occurrence.scheduled_for, occurrence.id
LIMIT 16
