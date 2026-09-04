-- name: managed_configuration_validate_revision :one
UPDATE control_plane.managed_configuration_revisions AS validated
SET state = @state, validation_diagnostics = @diagnostics::jsonb, validated_at = clock_timestamp()
WHERE id = @revision_id::uuid AND state IN ('DRAFT', 'INVALID')
RETURNING id::text, ref, revision, state, content_format, content, digest,
          COALESCE((SELECT ref FROM control_plane.managed_configuration_revisions parent WHERE parent.id = validated.parent_revision_id), ''),
          validation_diagnostics, created_at, validated_at, published_at;
