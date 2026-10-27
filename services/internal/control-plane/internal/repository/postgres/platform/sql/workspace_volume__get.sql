-- name: workspace_volume__get :one
SELECT v.id::text,v.ref,v.generation,v.version,v.state,v.create_task_ref,
 COALESCE(v.preparation_task_ref,''),COALESCE(v.pvc_uid,''),COALESCE(v.ready_record,'')
FROM control_plane.session_workspace_volumes v
JOIN control_plane.session_workspace_storage w ON w.id=v.workspace_id
WHERE w.organization_id=@organization_id::uuid AND w.ref=@workspace_ref
FOR UPDATE OF v;
