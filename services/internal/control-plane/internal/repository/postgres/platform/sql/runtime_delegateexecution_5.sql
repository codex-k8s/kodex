-- name: platform__runtime_delegateexecution_5 :one
SELECT next_turn_number FROM control_plane.sessions WHERE id=$1::uuid FOR UPDATE
