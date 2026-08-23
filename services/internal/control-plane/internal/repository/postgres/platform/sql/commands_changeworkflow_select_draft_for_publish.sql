-- name: commands_changeworkflow_select_draft_for_publish :one
SELECT draft_spec,published_version+1 FROM control_plane.workflows WHERE id=$1::uuid
