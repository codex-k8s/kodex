-- name: kubernetes_file__list :many
SELECT source.ref FROM control_plane.kubernetes_file_input_sources source
WHERE source.organization_id=@organization_id::uuid AND source.invocation_id=@invocation_id::uuid
ORDER BY source.created_at,source.ref LIMIT 257;
