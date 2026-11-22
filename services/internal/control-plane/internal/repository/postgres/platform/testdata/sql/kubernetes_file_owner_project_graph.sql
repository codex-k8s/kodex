-- name: kubernetes_file_owner_project_graph :one
SELECT source.project_id::text,source.id::text,source.invocation_id::text,
 source.prepared_content_id::text,source.stream_id::text
FROM control_plane.kubernetes_file_input_sources source
WHERE source.organization_id=$1::uuid AND source.ref=$2;
