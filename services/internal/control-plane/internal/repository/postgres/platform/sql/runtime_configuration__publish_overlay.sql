-- name: runtime_configuration__publish_overlay :one
WITH inserted AS (
    INSERT INTO control_plane.agent_config_overlay_versions
        (ref, organization_id, agent_id, version_number, parent_version_id, state, content, digest,
         validation_errors, created_by, validated_at, published_at, diagnostics, schema_revision, schema_digest)
    SELECT @ref, @organization_id::uuid, @agent_id::uuid,
           (SELECT max(existing.version_number) + 1 FROM control_plane.agent_config_overlay_versions existing WHERE existing.agent_id = @agent_id::uuid),
           @draft_id::uuid, 'PUBLISHED', @content, @digest, '[]'::jsonb,
           @created_by::uuid, clock_timestamp(), clock_timestamp(), '[]'::jsonb, @schema_revision, @schema_digest
    WHERE EXISTS (
        SELECT 1
        FROM control_plane.agent_config_overlay_versions draft
        WHERE draft.id = @draft_id::uuid
          AND draft.agent_id = @agent_id::uuid
          AND draft.state = 'SUPERSEDED'
    )
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
