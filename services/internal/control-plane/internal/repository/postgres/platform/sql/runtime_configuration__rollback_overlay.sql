-- name: runtime_configuration__rollback_overlay :one
WITH source AS (
    SELECT overlay_version.id, overlay_version.content, overlay_version.digest
    FROM control_plane.agent_config_overlay_versions overlay_version
    WHERE overlay_version.agent_id = @agent_id::uuid
      AND overlay_version.ref = @source_ref
      AND overlay_version.state IN ('PUBLISHED', 'SUPERSEDED')
), inserted AS (
    INSERT INTO control_plane.agent_config_overlay_versions
        (ref, organization_id, agent_id, version_number, parent_version_id, state, content, digest,
         validation_errors, created_by, validated_at, published_at)
    SELECT @ref, @organization_id::uuid, @agent_id::uuid,
           (SELECT max(existing.version_number) + 1 FROM control_plane.agent_config_overlay_versions existing WHERE existing.agent_id = @agent_id::uuid),
           source.id, 'PUBLISHED', source.content, source.digest, '[]'::jsonb,
           @created_by::uuid, clock_timestamp(), clock_timestamp()
    FROM source
    RETURNING id, ref
), updated_agent AS (
    UPDATE control_plane.agents agent
    SET current_config_overlay_id = inserted.id,
        updated_at = clock_timestamp()
    FROM inserted
    WHERE agent.id = @agent_id::uuid
    RETURNING agent.id
)
SELECT inserted.ref FROM inserted JOIN updated_agent ON true;
