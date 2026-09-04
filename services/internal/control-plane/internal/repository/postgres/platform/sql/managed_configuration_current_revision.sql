-- name: managed_configuration_current_revision :one
SELECT revision.id::text, revision.ref, revision.revision, revision.state, revision.content_format,
       revision.content, revision.digest, COALESCE(parent.ref, ''), revision.validation_diagnostics,
       revision.created_at, revision.validated_at, revision.published_at
FROM control_plane.managed_configuration_revisions revision
LEFT JOIN control_plane.managed_configuration_revisions parent ON parent.id=revision.parent_revision_id
WHERE revision.organization_id=$1::uuid AND revision.configuration_set_id=$2::uuid AND revision.id=$3::uuid;
