-- name: kubernetes_cleanup_finish :one
SELECT control_plane.kubernetes_stage_cleanup_complete(@id::uuid,@kind,@owner,@generation,@version,@etag,@success);
