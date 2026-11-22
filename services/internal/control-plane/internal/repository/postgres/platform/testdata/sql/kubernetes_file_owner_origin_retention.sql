-- name: kubernetes_file_owner_origin_retention :one
SELECT origin.artifact_revision_id IS NOT NULL,control_plane.kubernetes_file_origin_retained(origin.artifact_id)
FROM control_plane.kubernetes_file_input_sources source
JOIN control_plane.kubernetes_file_input_origins origin ON origin.invocation_id=source.invocation_id AND origin.organization_id=source.organization_id
WHERE source.ref=$1;
