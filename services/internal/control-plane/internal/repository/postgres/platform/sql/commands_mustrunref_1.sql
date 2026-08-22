-- name: platform__commands_mustrunref_1 :one
SELECT ref FROM control_plane.runs WHERE id=$1::uuid
