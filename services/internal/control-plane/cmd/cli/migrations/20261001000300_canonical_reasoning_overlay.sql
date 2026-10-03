-- +goose Up
SET ROLE control_plane_owner;

-- Миграция 20261001000100 записала разделитель строки как два литеральных
-- символа `\n`. Сохраняем immutable history и публикуем каноническую дочернюю
-- версию только для текущих overlay, затронутых этой точной формой строки.
CREATE TEMP TABLE issue_1789_overlay_canonical_targets ON COMMIT DROP AS
SELECT agent.id AS agent_id,
       current.id AS parent_id,
       current.organization_id,
       (SELECT max(existing.version_number) + 1
          FROM control_plane.agent_config_overlay_versions existing
         WHERE existing.agent_id = agent.id) AS next_version,
       replace(
         replace(
           current.content,
           E'\\nmodel_reasoning_effort = "medium"',
           E'\nmodel_reasoning_effort = "medium"'
         ),
         E'model_reasoning_effort = "medium"\\n',
         E'model_reasoning_effort = "medium"\n'
       ) AS content,
       current.created_by,
       current.schema_revision,
       current.schema_digest
FROM control_plane.agents agent
JOIN control_plane.agent_config_overlay_versions current
  ON current.id = agent.current_config_overlay_id
WHERE strpos(current.content, E'model_reasoning_effort = "medium"\\n') > 0
   OR strpos(current.content, E'\\nmodel_reasoning_effort = "medium"') > 0;

UPDATE control_plane.agent_config_overlay_versions current
SET state = 'SUPERSEDED'
FROM issue_1789_overlay_canonical_targets target
WHERE current.id = target.parent_id;

WITH inserted AS (
    INSERT INTO control_plane.agent_config_overlay_versions
        (ref, organization_id, agent_id, version_number, parent_version_id, state,
         content, digest, validation_errors, diagnostics, schema_revision,
         schema_digest, created_by, validated_at, published_at)
    SELECT 'cov_' || replace(gen_random_uuid()::text, '-', ''),
           organization_id,
           agent_id,
           next_version,
           parent_id,
           'PUBLISHED',
           content,
           encode(public.digest(convert_to(content, 'UTF8'), 'sha256'), 'hex'),
           '[]'::jsonb,
           '[]'::jsonb,
           schema_revision,
           schema_digest,
           created_by,
           clock_timestamp(),
           clock_timestamp()
    FROM issue_1789_overlay_canonical_targets
    RETURNING id, agent_id
)
UPDATE control_plane.agents agent
SET current_config_overlay_id = inserted.id,
    version = agent.version + 1,
    updated_at = clock_timestamp()
FROM inserted
WHERE agent.id = inserted.agent_id;

RESET ROLE;
