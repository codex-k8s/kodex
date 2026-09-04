-- name: managed_configuration_insert_revision :one
INSERT INTO control_plane.managed_configuration_revisions AS inserted
    (ref, organization_id, configuration_set_id, revision, state, content_format, content,
     digest, parent_revision_id, created_by)
SELECT @revision_ref, @organization_id::uuid, @configuration_set_id::uuid,
       COALESCE(max(revision), 0) + 1, 'DRAFT', @content_format, @content, @digest,
       NULLIF(@parent_revision_id, '')::uuid, @actor_id::uuid
FROM control_plane.managed_configuration_revisions
WHERE configuration_set_id = @configuration_set_id::uuid
RETURNING id::text, ref, revision, state, content_format, content, digest,
          COALESCE((SELECT ref FROM control_plane.managed_configuration_revisions parent WHERE parent.id = inserted.parent_revision_id), ''),
          validation_diagnostics, created_at, validated_at, published_at;
