-- name: configuration_addassistantturncommand_select_sessions_id :one
SELECT next_turn_number FROM control_plane.sessions WHERE id=$1::uuid FOR UPDATE
