-- name: bootstrap_component_disable_system_assistant :exec
UPDATE control_plane.agents
SET enabled = false,
    state = 'DISABLED',
    updated_at = clock_timestamp()
WHERE system_key = 'system-assistant';
