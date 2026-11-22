-- name: kubernetes_operator__target :one
SELECT content_sha256,payload,cluster_uid::text FROM control_plane.kubernetes_target_revisions WHERE ref=@ref AND revision=@revision;
