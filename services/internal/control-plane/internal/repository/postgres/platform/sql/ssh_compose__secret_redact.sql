-- name: ssh_compose__secret_redact :exec
UPDATE control_plane.runtime_secret_operations
SET terminal_secret_snapshot=(terminal_secret_snapshot-'DisplayHint'-'NextActions')||jsonb_build_object('Purpose','SSH_COMPOSE')
WHERE secret_id=@secret_id::uuid AND terminal_secret_snapshot IS NOT NULL;
