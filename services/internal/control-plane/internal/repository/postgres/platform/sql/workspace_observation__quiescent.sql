-- name: workspace_observation__quiescent :one
UPDATE control_plane.session_workspace_storage
SET state='CHECKPOINT_PENDING',quiescence_record=@record,block_reason='',version=version+1,updated_at=clock_timestamp()
WHERE id=@workspace_id::uuid AND organization_id=@organization_id::uuid
    AND generation=@workspace_generation AND version=@version
    AND state IN ('WRITING','QUIESCING','BLOCKED') AND writer_lease_id=@lease_id::uuid
    AND writer_acknowledgement=@ack AND quiescence_record IS NULL
RETURNING version;
