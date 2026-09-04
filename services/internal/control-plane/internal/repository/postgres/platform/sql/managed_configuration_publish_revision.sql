-- name: managed_configuration_publish_revision :one
WITH superseded AS (
    UPDATE control_plane.managed_configuration_revisions revision
    SET state = 'SUPERSEDED'
    FROM control_plane.managed_configuration_sets configuration
    WHERE configuration.id = @configuration_set_id::uuid
      AND revision.id = configuration.current_revision_id AND revision.state = 'PUBLISHED'
), published AS (
    UPDATE control_plane.managed_configuration_revisions
    SET state = 'PUBLISHED', published_at = clock_timestamp()
    WHERE id = @revision_id::uuid AND configuration_set_id = @configuration_set_id::uuid AND state = 'VALID'
    RETURNING *
), updated AS (
    UPDATE control_plane.managed_configuration_sets
    SET current_revision_id = (SELECT id FROM published), version = version + 1, updated_at = clock_timestamp()
    WHERE id = @configuration_set_id::uuid AND version = @expected_version AND EXISTS (SELECT 1 FROM published)
    RETURNING version, updated_at
)
SELECT published.id::text, published.ref, published.revision, published.state, published.content_format,
       published.content, published.digest,
       COALESCE((SELECT ref FROM control_plane.managed_configuration_revisions parent WHERE parent.id = published.parent_revision_id), ''),
       published.validation_diagnostics, published.created_at, published.validated_at, published.published_at,
       updated.version, updated.updated_at
FROM published JOIN updated ON true;
