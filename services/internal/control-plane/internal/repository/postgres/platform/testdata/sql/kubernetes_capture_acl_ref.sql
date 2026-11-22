-- name: kubernetes_capture_acl_ref :one
SELECT capture.ref FROM control_plane.kubernetes_output_captures capture
JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
WHERE stream.ref=$1 AND capture.organization_id=$2::uuid ORDER BY capture.capture_generation DESC LIMIT 1;
