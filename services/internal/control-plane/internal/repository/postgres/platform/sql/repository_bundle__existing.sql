-- name: repository_bundle__existing :one
SELECT ref,input_sha256
FROM control_plane.repository_bundle_publications
WHERE organization_id=@organization_id::uuid AND origin_runtime_revision_id=@runtime_revision_id::uuid
  AND origin_node_id=@node_id::uuid AND idempotency_key=@idempotency_key;
