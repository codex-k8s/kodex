-- name: runtime_completeexecution_update_runs_state_version_updated_at :exec
UPDATE control_plane.runs SET state='WAITING_HUMAN',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid
