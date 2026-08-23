-- name: commands_changeworkflow_update_workflows_published_spec_published_version_state :exec
UPDATE control_plane.workflows SET published_spec=$2,published_version=$3,state='PUBLISHED',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
