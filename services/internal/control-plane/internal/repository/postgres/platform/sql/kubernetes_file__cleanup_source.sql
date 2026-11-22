-- name: kubernetes_file__cleanup_source :one
SELECT source.id::text,coalesce(source.stream_id::text,'')
FROM control_plane.kubernetes_file_input_sources source
WHERE source.ref=@source_ref AND source.organization_id=@organization_id::uuid
 AND source.invocation_id=@invocation_id::uuid AND source.actor_id=@actor_id::uuid
 AND source.project_id=@project_id::uuid;
