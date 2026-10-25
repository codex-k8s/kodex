-- name: repository_bundle__invocation_origin :one
SELECT publication.ref,publication.origin_node_id::text
FROM control_plane.repository_bundle_publications publication
WHERE publication.organization_id=@organization_id::uuid AND publication.integration_invocation_id=@invocation_id::uuid;
