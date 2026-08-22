-- name: platform__commands_changeworkflow_6 :one
SELECT draft_spec,published_version+1 FROM control_plane.workflows WHERE id=$1::uuid
