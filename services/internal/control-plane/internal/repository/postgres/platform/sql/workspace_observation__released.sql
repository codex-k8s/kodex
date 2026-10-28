-- name: workspace_observation__released :one
UPDATE control_plane.session_workspace_storage
SET writer_released=true,version=version+1,updated_at=clock_timestamp()
WHERE id=@workspace_id::uuid AND organization_id=@organization_id::uuid
    AND generation=@workspace_generation AND version=@version AND writer_lease_id=@lease_id::uuid
    AND writer_acknowledgement=@ack AND quiescence_record=@record
    AND state='CHECKPOINT_PENDING' AND NOT writer_released
RETURNING version;
