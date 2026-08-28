-- name: runtime_configuration__create_overlay_draft :one
WITH superseded AS (
    UPDATE control_plane.agent_config_overlay_versions overlay_version
    SET state = 'SUPERSEDED'
    WHERE overlay_version.agent_id = @agent_id::uuid
      AND overlay_version.state IN ('DRAFT', 'VALID', 'INVALID')
), inserted AS (
    INSERT INTO control_plane.agent_config_overlay_versions
        (ref, organization_id, agent_id, version_number, parent_version_id, state, content, digest, created_by)
    SELECT @ref, @organization_id::uuid, @agent_id::uuid,
           COALESCE(max(existing.version_number), 0) + 1,
           @parent_version_id::uuid,
           'DRAFT', @content, @digest, @created_by::uuid
    FROM control_plane.agent_config_overlay_versions existing
    WHERE existing.agent_id = @agent_id::uuid
    RETURNING ref
)
SELECT ref FROM inserted;
