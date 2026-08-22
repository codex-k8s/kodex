-- name: platform__commands_changeworkflow_4 :one
SELECT draft_spec FROM control_plane.workflows WHERE id=$1::uuid
