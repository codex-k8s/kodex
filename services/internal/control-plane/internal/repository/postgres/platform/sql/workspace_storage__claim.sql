-- name: workspace_storage__claim :one
UPDATE control_plane.session_workspace_storage workspace
SET state='WRITER_PENDING',writer_lease_id=lease.id,version=workspace.version+1,updated_at=clock_timestamp()
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
WHERE workspace.organization_id=@organization_id::uuid AND workspace.ref=@workspace_ref
    AND workspace.generation=@workspace_generation AND workspace.state IN ('EMPTY','RESTORED')
    AND workspace.writer_lease_id IS NULL AND workspace.writer_acknowledgement IS NULL
    AND workspace.quiescence_record IS NULL AND workspace.destructive_task_id IS NULL
    AND lease.organization_id=workspace.organization_id AND lease.ref=@lease_ref
    AND lease.state='CLAIMED' AND lease.expires_at>statement_timestamp()
    AND revision.organization_id=workspace.organization_id AND revision.session_id=workspace.session_id
    AND revision.project_id=workspace.project_id AND revision.agent_id=workspace.agent_id
    AND revision.generation=lease.generation
RETURNING workspace.version;
