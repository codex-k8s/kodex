-- name: platform__commands_emitrunevent_2 :one
SELECT ref FROM control_plane.projects WHERE id=$1::uuid
