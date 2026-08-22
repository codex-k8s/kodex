-- name: commands_changeworkflow_select_draft_for_validation :one
SELECT draft_spec FROM control_plane.workflows WHERE id=$1::uuid
