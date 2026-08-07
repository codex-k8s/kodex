-- name: TurnAttemptSave
INSERT INTO control_plane.turn_attempts (
    turn_id,
    attempt,
    workload_id,
    authority_generation,
    state,
    input_sha256,
    lease_fence,
    runtime_revision_id,
    runtime_revision_version,
    started_at
) SELECT
    @turn_id::uuid,
    @attempt,
    @workload_id,
    @authority_generation,
    @state,
    @input_sha256,
    @lease_fence,
    revision.id,
    revision.version,
    @started_at
FROM control_plane.resources AS turn
JOIN control_plane.resources AS revision
  ON revision.organization_id = turn.organization_id
 AND revision.project_id = turn.project_id
 AND revision.owner_actor_id = turn.owner_actor_id
 AND revision.id = (turn.spec ->> 'runtimeRevisionId')::uuid
 AND revision.kind = 'RUNTIME_REVISION'
 AND revision.state <> 'DELETED'
WHERE turn.id = @turn_id::uuid
  AND turn.kind = 'TURN'
  AND turn.state <> 'DELETED'
  AND (turn.spec ->> 'attempt')::integer = @attempt
ON CONFLICT (turn_id, attempt) DO UPDATE
SET
    workload_id = excluded.workload_id,
    authority_generation = excluded.authority_generation,
    state = excluded.state,
    lease_fence = excluded.lease_fence,
    started_at = excluded.started_at
WHERE control_plane.turn_attempts.state = 'QUEUED'
  AND control_plane.turn_attempts.input_sha256 = excluded.input_sha256
  AND control_plane.turn_attempts.runtime_revision_id = excluded.runtime_revision_id
  AND control_plane.turn_attempts.runtime_revision_version = excluded.runtime_revision_version
