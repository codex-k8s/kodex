-- name: kubernetes_operator__empty :one
SELECT NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_operator_registry_revisions);
