-- name: repository_bundle_source_retention :one
SELECT control_plane.artifact_active_binding_count(head.id),control_plane.artifact_has_retained_revisions(head.id)
FROM control_plane.artifact_heads head
JOIN control_plane.repository_bundle_publications publication ON publication.ref=$2 AND publication.organization_id=head.organization_id
WHERE head.ref=$1;
