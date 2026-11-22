-- name: kubernetes_cleanup_prepared_record :one
SELECT control_plane.prepared_content_record_receipt($1,$2,$3,$4,$5,$6,$7,$8);
