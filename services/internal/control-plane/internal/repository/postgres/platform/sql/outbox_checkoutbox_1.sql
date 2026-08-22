-- name: platform__outbox_checkoutbox_1 :one
SELECT to_regclass('control_plane.outbox_events')::text
