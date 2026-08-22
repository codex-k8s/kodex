-- name: platform__commands_changeworkflow_update_workflows_draft_spec_state_version :exec
UPDATE control_plane.workflows SET draft_spec=$2,state='DRAFT',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
