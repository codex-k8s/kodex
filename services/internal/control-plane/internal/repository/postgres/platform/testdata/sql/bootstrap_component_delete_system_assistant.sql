-- name: bootstrap_component_delete_system_assistant :exec
DELETE FROM control_plane.agents
WHERE system_key = 'system-assistant';
