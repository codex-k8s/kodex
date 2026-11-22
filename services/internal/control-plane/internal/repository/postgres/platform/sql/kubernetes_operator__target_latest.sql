-- name: kubernetes_operator__target_latest :one
SELECT revision,content_sha256,payload FROM control_plane.kubernetes_target_revisions WHERE ref=@ref ORDER BY revision DESC LIMIT 1;
