-- name: kubernetes_file_owner_cleanup_complete :one
SELECT control_plane.kubernetes_stage_cleanup_complete($1,'KUBERNETES_FILE_SOURCE',$2,$3,$4,$5,$6);
