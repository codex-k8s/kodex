-- name: platform__runtime_completeexecution_12 :one
SELECT next_turn_number FROM control_plane.sessions WHERE id=$1::uuid FOR UPDATE
