-- name: kubernetes_cleanup_record :one
SELECT control_plane.kubernetes_stage_cleanup_record(@id::uuid,@kind,@owner,@generation,@key,@version,@etag,@digest,@size);
