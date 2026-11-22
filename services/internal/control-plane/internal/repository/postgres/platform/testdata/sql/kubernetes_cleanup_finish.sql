-- name: kubernetes_cleanup_finish :one
SELECT CASE WHEN $2::text='KUBERNETES_PREPARED'
 THEN control_plane.prepared_content_finish($1,$3,$4,$7,$5,$6)
 ELSE control_plane.kubernetes_stage_cleanup_complete($1,$2,$3,$4,$5,$6,$7) END;
