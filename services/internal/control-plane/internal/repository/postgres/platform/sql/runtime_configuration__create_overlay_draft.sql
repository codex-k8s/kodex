-- name: runtime_configuration__create_overlay_draft :one
WITH inserted AS (
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
