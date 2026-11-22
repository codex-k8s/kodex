-- name: kubernetes_execution__child_current :one
SELECT control_plane.kubernetes_execution_child_grant_current(@child_id::uuid,@invocation_id::uuid);
