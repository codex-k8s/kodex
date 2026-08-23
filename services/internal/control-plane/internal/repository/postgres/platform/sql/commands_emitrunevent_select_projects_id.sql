-- name: commands_emitrunevent_select_projects_id :one
SELECT ref FROM control_plane.projects WHERE id=$1::uuid
