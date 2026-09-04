-- name: commands_sync_agent_avatar :one
WITH target AS (
    SELECT agent.id, agent.project_id, agent.ref, agent.avatar_artifact_id
    FROM control_plane.agents AS agent
    WHERE agent.organization_id = @organization_id::uuid
      AND agent.ref = @agent_ref
      AND agent.system_key IS NULL
    FOR UPDATE
), candidate AS (
    SELECT artifact.id, artifact.ref, artifact.revision
    FROM target
    LEFT JOIN control_plane.artifacts AS artifact
      ON @artifact_ref <> ''
     AND artifact.organization_id = @organization_id::uuid
     AND artifact.project_id = target.project_id
     AND artifact.ref = @artifact_ref
     AND artifact.lifecycle_state = 'ACTIVE'
     AND artifact.scan_state = 'CLEAN'
     AND artifact.preview_state = 'AVAILABLE'
     AND artifact.media_type IN ('image/jpeg', 'image/png', 'image/webp')
     AND artifact.size_bytes BETWEEN 1 AND 5242880
     AND NOT EXISTS (
         SELECT 1
         FROM control_plane.artifact_bindings AS existing_binding
         WHERE existing_binding.artifact_id = artifact.id
           AND NOT (
               existing_binding.target_kind = 'AGENT'
               AND existing_binding.target_ref = target.ref
           )
     )
    WHERE @artifact_ref = '' OR artifact.id IS NOT NULL
), removed_binding AS (
    DELETE FROM control_plane.artifact_bindings AS binding
    USING target, candidate
    WHERE binding.artifact_id = target.avatar_artifact_id
      AND target.avatar_artifact_id IS DISTINCT FROM candidate.id
      AND binding.target_kind = 'AGENT'
      AND binding.target_ref = target.ref
), retired_artifact AS (
    UPDATE control_plane.artifacts AS artifact
    SET lifecycle_state = 'DELETED',
        deleted_at = clock_timestamp(),
        purge_after = clock_timestamp() + interval '30 days',
        purged_at = NULL,
        version = artifact.version + 1
    FROM target, candidate
    WHERE artifact.id = target.avatar_artifact_id
      AND target.avatar_artifact_id IS DISTINCT FROM candidate.id
      AND artifact.lifecycle_state = 'ACTIVE'
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.artifact_bindings AS other_binding
          WHERE other_binding.artifact_id = artifact.id
            AND NOT (
                other_binding.target_kind = 'AGENT'
                AND other_binding.target_ref = target.ref
            )
      )
), inserted_binding AS (
    INSERT INTO control_plane.artifact_bindings
        (artifact_id, target_kind, target_ref, created_by)
    SELECT candidate.id, 'AGENT', target.ref, @actor_id::uuid
    FROM candidate
    JOIN target ON true
    WHERE candidate.id IS NOT NULL
    ON CONFLICT (artifact_id, target_kind, target_ref) DO NOTHING
), updated_agent AS (
    UPDATE control_plane.agents AS agent
    SET avatar_artifact_id = candidate.id,
        avatar_artifact_revision = candidate.revision
    FROM target, candidate
    WHERE agent.id = target.id
    RETURNING agent.id
)
SELECT COALESCE(candidate.ref, ''), COALESCE(candidate.revision, 0)
FROM candidate
JOIN updated_agent ON true;
