-- name: platform__configuration_addassistantturncommand_3 :one
SELECT next_turn_number FROM control_plane.sessions WHERE id=$1::uuid FOR UPDATE
