-- name: kubernetes_manual_purge_invalid_budget :many
SELECT * FROM control_plane.artifact_manual_retention_claim($1,$2,$3);
