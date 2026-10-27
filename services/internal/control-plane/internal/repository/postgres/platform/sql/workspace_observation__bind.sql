-- name: workspace_observation__bind :one
UPDATE control_plane.session_workspace_storage
SET state='WRITING',writer_acknowledgement=@ack,version=version+1,updated_at=clock_timestamp()
WHERE id=@workspace_id::uuid AND organization_id=@organization_id::uuid
    AND generation=@workspace_generation AND version=@version AND state='WRITER_PENDING'
    AND writer_lease_id=@lease_id::uuid AND writer_acknowledgement IS NULL AND quiescence_record IS NULL
RETURNING version;
