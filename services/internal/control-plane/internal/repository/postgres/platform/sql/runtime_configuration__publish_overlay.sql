-- name: runtime_configuration__publish_overlay :one
WITH superseded_published AS (
    UPDATE control_plane.agent_config_overlay_versions published
    SET state = 'SUPERSEDED'
    WHERE published.agent_id = @agent_id::uuid AND published.state = 'PUBLISHED'
), inserted AS (
    INSERT INTO control_plane.agent_config_overlay_versions
        (ref, organization_id, agent_id, version_number, parent_version_id, state, content, digest,
         validation_errors, created_by, validated_at, published_at)
    SELECT @ref, @organization_id::uuid, @agent_id::uuid,
           (SELECT max(existing.version_number) + 1 FROM control_plane.agent_config_overlay_versions existing WHERE existing.agent_id = @agent_id::uuid),
           @draft_id::uuid, 'PUBLISHED', @content, @digest, '[]'::jsonb,
           @created_by::uuid, clock_timestamp(), clock_timestamp()
    FROM superseded_published
    UNION ALL
    SELECT @ref, @organization_id::uuid, @agent_id::uuid,
           (SELECT max(existing.version_number) + 1 FROM control_plane.agent_config_overlay_versions existing WHERE existing.agent_id = @agent_id::uuid),
           @draft_id::uuid, 'PUBLISHED', @content, @digest, '[]'::jsonb,
           @created_by::uuid, clock_timestamp(), clock_timestamp()
    WHERE NOT EXISTS (SELECT 1 FROM superseded_published)
    RETURNING id, ref
), superseded_draft AS (
    UPDATE control_plane.agent_config_overlay_versions draft
    SET state = 'SUPERSEDED'
    WHERE draft.id = @draft_id::uuid AND draft.state = 'VALID'
    RETURNING draft.id
), updated_agent AS (
    UPDATE control_plane.agents agent
    SET current_config_overlay_id = inserted.id,
        version = agent.version + 1,
        updated_at = clock_timestamp()
    FROM inserted, superseded_draft
    WHERE agent.id = @agent_id::uuid
    RETURNING agent.id
)
SELECT inserted.ref FROM inserted JOIN updated_agent ON true;
