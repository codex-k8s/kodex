-- name: workspace_volume__attempt :one
SELECT a.input_snapshot,a.input_digest,a.lease_ref,a.grant_state,a.expires_at,
 a.expires_at>clock_timestamp(),a.reported_at IS NOT NULL,
 COALESCE(a.observed_pvc_uid,''),COALESCE(a.job_uid,''),COALESCE(a.pod_uid,''),COALESCE(a.ready_record,'')
FROM control_plane.session_workspace_volume_attempts a
JOIN control_plane.session_workspace_volume_tasks t ON t.id=a.task_id
JOIN control_plane.session_workspace_volumes v ON v.id=t.volume_id
JOIN control_plane.session_workspace_storage w ON w.id=v.workspace_id
WHERE w.organization_id=@organization_id::uuid AND t.id=@task_id::uuid AND t.ref=@task_ref
 AND a.lease_ref=@lease_ref AND a.attempt=@generation::integer AND a.fence_digest=@fence_digest
FOR UPDATE OF a;
