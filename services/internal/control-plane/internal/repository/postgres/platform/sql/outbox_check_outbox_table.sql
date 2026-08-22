-- name: platform__outbox_check_outbox_table :one
SELECT to_regclass('control_plane.outbox_events')::text
