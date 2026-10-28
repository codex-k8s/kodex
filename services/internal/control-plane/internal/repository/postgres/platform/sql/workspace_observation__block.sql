-- name: workspace_observation__block :one
UPDATE control_plane.session_workspace_storage
SET state='BLOCKED',block_reason=CASE WHEN block_reason='' THEN @reason ELSE block_reason END,
    version=version+1,updated_at=clock_timestamp()
WHERE id=@workspace_id::uuid AND organization_id=@organization_id::uuid
    AND generation=@workspace_generation AND version=@version AND writer_lease_id=@lease_id::uuid
    AND writer_acknowledgement=@ack AND quiescence_record IS NULL
    AND state IN ('WRITING','QUIESCING','BLOCKED')
RETURNING block_reason;
