-- name: managed_configuration_copy :one
WITH source AS (
    SELECT configuration.organization_id, configuration.project_id, configuration.kind,
           revision.content_format, revision.content, revision.digest
    FROM control_plane.managed_configuration_sets configuration
    JOIN control_plane.managed_configuration_revisions revision ON revision.id = configuration.current_revision_id
    WHERE configuration.organization_id = @organization_id::uuid AND configuration.ref = @configuration_ref
      AND configuration.managed_by = 'GIT' AND configuration.version = @expected_version
), inserted_set AS (
    INSERT INTO control_plane.managed_configuration_sets
        (ref, organization_id, project_id, kind, name, managed_by, source, created_by)
    SELECT @copy_ref, organization_id, project_id, kind, @name, 'UI', 'control-center', @actor_id::uuid FROM source
    RETURNING id, ref, project_id, kind, name, managed_by, source, source_revision, version, updated_at
), inserted_revision AS (
    INSERT INTO control_plane.managed_configuration_revisions
        (ref, organization_id, configuration_set_id, revision, state, content_format, content, digest, parent_revision_id, created_by)
    SELECT @revision_ref, source.organization_id, inserted_set.id, 1, 'DRAFT', source.content_format,
           source.content, source.digest, NULL, @actor_id::uuid
    FROM source JOIN inserted_set ON true RETURNING *
)
SELECT inserted_set.id::text, inserted_set.ref, COALESCE(inserted_set.project_id::text, ''),
       COALESCE((SELECT ref FROM control_plane.projects WHERE id = inserted_set.project_id), ''),
       inserted_set.kind, inserted_set.name, inserted_set.managed_by, inserted_set.source,
       inserted_set.source_revision, inserted_set.version, inserted_set.updated_at,
       inserted_revision.id::text, inserted_revision.ref, inserted_revision.revision,
       inserted_revision.state, inserted_revision.content_format, inserted_revision.content,
       inserted_revision.digest,
       COALESCE((SELECT ref FROM control_plane.managed_configuration_revisions WHERE id = inserted_revision.parent_revision_id), ''),
       inserted_revision.validation_diagnostics, inserted_revision.created_at,
       inserted_revision.validated_at, inserted_revision.published_at
FROM inserted_set JOIN inserted_revision ON true;
