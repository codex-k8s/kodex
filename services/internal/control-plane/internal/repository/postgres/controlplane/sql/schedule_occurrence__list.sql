-- name: ScheduleOccurrenceList
SELECT
    id::text,
    schedule_id::text,
    organization_id::text,
    project_id::text,
    scheduled_for,
    target_resource_id::text,
    target_kind,
    target_version,
    effective_input_sha256,
    prompt_profile_id::text,
    prompt_revision,
    runtime_revision_id::text,
    session_policy,
    coalesce(room_id::text, ''),
    notification_policy,
    maximum_execution_duration_ms,
    coalesce,
    overlap_policy,
    maximum_attempts,
    initial_backoff_ms,
    maximum_backoff_ms,
    dead_letter_at,
    state,
    version,
    attempt,
    coalesce(claimant_workload_id, ''),
    coalesce(authority_generation, 0),
    coalesce(token_hash, ''),
    coalesce(claim_key_sha256, ''),
    coalesce(lease_expires_at, 'epoch'::timestamptz),
    available_at,
    coalesce(outcome, ''),
    coalesce(result_artifact_id::text, ''),
    coalesce(recovery_evidence_sha256, ''),
    coalesce(recovery_blocked_at, 'epoch'::timestamptz),
    coalesce(execution_session_id::text, ''),
    coalesce(execution_session_version, 0),
    coalesce(execution_turn_id::text, ''),
    coalesce(execution_turn_version, 0),
    coalesce(execution_process_run_id::text, ''),
    coalesce(execution_process_version, 0),
    coalesce(execution_runtime_revision_id::text, ''),
    coalesce(execution_runtime_revision_version, 0),
    claimed_at,
    updated_at
FROM control_plane.schedule_occurrences
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND schedule_id = @schedule_id::uuid
  AND (cardinality(@states::text[]) = 0 OR state = ANY(@states::text[]))
  AND id > coalesce(
      nullif(@after_id, '')::uuid,
      '00000000-0000-0000-0000-000000000000'::uuid
  )
ORDER BY id
LIMIT @limit
