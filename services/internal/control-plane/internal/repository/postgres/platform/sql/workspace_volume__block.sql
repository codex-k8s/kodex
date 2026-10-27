-- name: workspace_volume__block :exec
UPDATE control_plane.session_workspace_volumes
SET state='BLOCKED',block_reason=@reason,version=version+1,updated_at=clock_timestamp()
WHERE id=@volume_id::uuid AND generation=@generation AND state NOT IN('READY','PURGED');
