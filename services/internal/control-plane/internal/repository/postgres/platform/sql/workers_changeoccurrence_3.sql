-- name: platform__workers_changeoccurrence_3 :one
SELECT id::text FROM control_plane.runs WHERE ref=$1
