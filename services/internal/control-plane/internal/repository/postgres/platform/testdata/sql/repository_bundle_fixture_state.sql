-- name: repository_bundle_fixture_state :one
SELECT publication.state,invocation.state,
       control_plane.artifact_has_retained_revisions(head.id),
       (SELECT count(*)::integer FROM control_plane.integration_invocations pending
        WHERE pending.id=publication.integration_invocation_id AND pending.lease_ref IS NOT NULL)
FROM control_plane.repository_bundle_publications publication
JOIN control_plane.integration_invocations invocation ON invocation.id=publication.integration_invocation_id
JOIN control_plane.artifact_revisions revision ON revision.ref=publication.archive_revision_ref
JOIN control_plane.artifact_heads head ON head.id=revision.artifact_id
WHERE publication.ref=$1;
