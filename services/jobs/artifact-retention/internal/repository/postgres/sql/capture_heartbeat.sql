-- name: capture_heartbeat :one
SELECT control_plane.kubernetes_capture_consumer_heartbeat(@owner,@revision::integer);
