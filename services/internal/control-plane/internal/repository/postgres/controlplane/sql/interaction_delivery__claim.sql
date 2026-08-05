WITH exhausted AS (
    UPDATE control_plane.interaction_delivery_work
       SET state = 'DEAD_LETTER', lease_owner = '', lease_token_sha256 = '',
           lease_expires_at = NULL, terminal_error_code = 'DELIVERY_RETRY_EXHAUSTED',
           next_action = 'OPERATOR_RECONCILIATION_REQUIRED', updated_at = clock_timestamp()
     WHERE organization_id = @organization_id AND project_id = @project_id
       AND state = 'CLAIMED' AND lease_expires_at <= clock_timestamp() AND attempts >= 32
    RETURNING id
), candidate AS (
    SELECT id FROM control_plane.interaction_delivery_work
    WHERE organization_id = @organization_id AND project_id = @project_id
      AND next_attempt_at <= clock_timestamp()
      AND (state = 'PENDING' OR (state = 'CLAIMED' AND lease_expires_at <= clock_timestamp()))
      AND attempts < 32
    ORDER BY created_at, id
    FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE control_plane.interaction_delivery_work work
SET state = 'CLAIMED', fence = work.fence + 1, lease_owner = @lease_owner,
    lease_token_sha256 = @lease_token_sha256,
    lease_expires_at = clock_timestamp() + @lease_duration::interval,
    attempts = work.attempts + 1, updated_at = clock_timestamp()
FROM candidate WHERE work.id = candidate.id
RETURNING work.id::text, work.organization_id::text, work.project_id::text, work.actor_id::text,
    work.session_id::text, work.session_version, work.turn_id::text, work.turn_version,
    work.attempt, work.runtime_revision_id::text, work.runtime_revision_version,
    work.immutable_input_sha256, work.kind, work.lifecycle_state, work.outcome,
    COALESCE(work.artifact_id::text, ''), COALESCE(work.artifact_version, 0), work.artifact_sha256,
    work.artifact_name, work.artifact_storage_ref, work.artifact_size_bytes, work.artifact_media_type,
    work.inline_payload, COALESCE(work.notification_room_id::text, ''),
    COALESCE(work.notification_policy, ''), COALESCE(work.scheduled_outcome, ''),
    work.fence, work.lease_expires_at;
