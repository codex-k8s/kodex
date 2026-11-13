-- name: workspace_unbound__retire :one
UPDATE control_plane.session_workspace_storage workspace
SET state=receipt.previous_state,writer_lease_id=NULL,version=workspace.version+1,updated_at=clock_timestamp()
FROM control_plane.workspace_unbound_stops receipt
WHERE workspace.id=@workspace_id::uuid AND workspace.organization_id=@organization_id::uuid
 AND workspace.generation=@workspace_generation AND workspace.version=@version
 AND workspace.writer_lease_id=@lease_id::uuid AND workspace.state='WRITER_PENDING'
 AND workspace.writer_acknowledgement IS NULL AND workspace.quiescence_record IS NULL
 AND receipt.lease_id=@lease_id::uuid AND receipt.workspace_id=workspace.id
 AND receipt.organization_id=workspace.organization_id AND receipt.record=@record
RETURNING workspace.state,receipt.record;
