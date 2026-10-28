-- name: workspace_restore__pvc :one
SELECT volume.pvc_uid,volume.generation,volume.preparation_task_ref,volume.ready_record
FROM control_plane.session_workspace_volumes volume
JOIN control_plane.session_workspace_storage workspace ON workspace.id=volume.workspace_id
WHERE workspace.organization_id=@organization_id::uuid AND workspace.id=@workspace_id::uuid
 AND volume.state='READY' AND volume.ready_record IS NOT NULL AND NOT volume.physical_deleted;
