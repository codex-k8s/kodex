-- name: managed_configuration_lock_revision :one
SELECT revision.id::text, revision.ref, revision.revision, revision.state, revision.content_format,
       revision.content, revision.digest, COALESCE(parent.ref, ''), revision.validation_diagnostics,
       revision.created_at, revision.validated_at, revision.published_at
FROM control_plane.managed_configuration_revisions revision
JOIN control_plane.managed_configuration_sets configuration ON configuration.id = revision.configuration_set_id
LEFT JOIN control_plane.managed_configuration_revisions parent ON parent.id = revision.parent_revision_id
WHERE configuration.organization_id = @organization_id::uuid
  AND configuration.id = @configuration_set_id::uuid AND revision.ref = @revision_ref
FOR UPDATE OF revision;
