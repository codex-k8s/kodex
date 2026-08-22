-- name: platform__commands_addsessionturn_3 :one
SELECT root_run_id::text FROM control_plane.runs WHERE ref=$1
