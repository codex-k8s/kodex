-- name: workspace_volume__task :one
-- Root -> session -> volume -> task: caller locks the preceding rows first.
SELECT t.id::text,t.ref,t.kind,t.state,t.attempt,t.frozen_input,t.origin_node_id::text,
 t.root_run_id::text,r.id::text,t.origin_actor_id::text,v.id::text,v.state,t.volume_generation,v.generation,
 w.ref,w.project_id::text,w.session_id::text,w.state,w.writer_lease_id IS NULL,
 t.available_at<=clock_timestamp(),EXISTS(
   SELECT 1 FROM control_plane.session_workspace_volume_attempts a
   WHERE a.task_id=t.id AND a.grant_state='ACTIVE' AND a.expires_at>clock_timestamp())
FROM control_plane.session_workspace_volume_tasks t
JOIN control_plane.session_workspace_volumes v ON v.id=t.volume_id
JOIN control_plane.session_workspace_storage w ON w.id=v.workspace_id
JOIN control_plane.run_nodes n ON n.id=t.origin_node_id AND n.organization_id=w.organization_id
JOIN control_plane.runs r ON r.id=n.run_id AND r.organization_id=w.organization_id
 AND r.project_id=w.project_id AND r.session_id=w.session_id AND r.root_run_id=t.root_run_id
WHERE w.organization_id=@organization_id::uuid AND t.ref=@task_ref
FOR UPDATE OF t;
