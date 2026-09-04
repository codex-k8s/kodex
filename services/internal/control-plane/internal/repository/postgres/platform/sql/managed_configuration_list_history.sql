-- name: managed_configuration_list_history :many
SELECT revision.id::text, revision.ref, revision.revision, revision.state, revision.content_format,
       revision.content, revision.digest, COALESCE(parent.ref, ''), revision.validation_diagnostics,
       revision.created_at, revision.validated_at, revision.published_at,
       (SELECT count(*)::bigint
        FROM control_plane.managed_configuration_revisions total_revision
        WHERE total_revision.configuration_set_id = configuration.id)
FROM control_plane.managed_configuration_revisions revision
JOIN control_plane.managed_configuration_sets configuration ON configuration.id = revision.configuration_set_id
LEFT JOIN control_plane.managed_configuration_revisions parent ON parent.id = revision.parent_revision_id
WHERE configuration.organization_id = @organization_id::uuid AND configuration.ref = @configuration_ref
  AND (@before_revision = 0 OR revision.revision < @before_revision)
ORDER BY revision.revision DESC
LIMIT @page_size;
