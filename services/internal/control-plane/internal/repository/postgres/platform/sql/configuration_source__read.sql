-- name: configuration_source__read :one
SELECT source.id::text, source.ref, source.version, source.generation, source.state,
       connection.ref, source.provider_key, source.repository_ref, source.ref_name, source.path,
       source.accepted_commit_sha, source.accepted_content_sha256, COALESCE(revision.ref,''),
       source.synced_at, source.failure_code, source.root_actor_id::text, source.content_format
FROM control_plane.managed_configuration_git_sources source
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=source.configuration_set_id AND configuration.organization_id=source.organization_id
JOIN control_plane.integration_connections connection ON connection.id=source.connection_id AND connection.organization_id=source.organization_id
LEFT JOIN control_plane.managed_configuration_revisions revision ON revision.id=source.accepted_revision_id AND revision.configuration_set_id=configuration.id
WHERE source.organization_id=$1::uuid AND configuration.ref=$2;
