-- name: command_deleted_access_target :one
SELECT resource_id, project_id, project_ref, owner_ref, related_refs, version
FROM (
 SELECT c.id::text resource_id, '' project_id, '' project_ref, s.ref owner_ref,
        '{}'::jsonb related_refs, c.version
 FROM control_plane.integration_connections c
 JOIN control_plane.subjects s ON s.id=c.created_by
 WHERE @kind='INTEGRATION' AND c.organization_id=@organization_id::uuid
   AND c.ref=@ref AND c.lifecycle_state='DELETED'
 UNION ALL
 SELECT c.id::text, p.id::text, p.ref, s.ref,
        jsonb_strip_nulls(jsonb_build_object('PROJECT',p.ref,
          'AGENT',CASE WHEN c.target_type='AGENT' THEN c.target_ref END,
          'WORKFLOW',CASE WHEN c.target_type='WORKFLOW' THEN c.target_ref END)), c.version
 FROM control_plane.schedules c
 JOIN control_plane.projects p ON p.id=c.project_id AND p.organization_id=c.organization_id
 JOIN control_plane.subjects s ON s.id=c.created_by
 WHERE @kind='SCHEDULE' AND c.organization_id=@organization_id::uuid
   AND c.ref=@ref AND c.lifecycle_state='DELETED'
 UNION ALL
 SELECT c.id::text, p.id::text, p.ref, s.ref, jsonb_build_object('PROJECT',p.ref), c.version
 FROM control_plane.runtime_environment_sets c
 JOIN control_plane.projects p ON p.id=c.project_id AND p.organization_id=c.organization_id
 JOIN control_plane.subjects s ON s.id=c.created_by
 WHERE @kind='RUNTIME_ENVIRONMENT' AND c.organization_id=@organization_id::uuid
   AND c.ref=@ref AND c.state='DELETED'
) target;
