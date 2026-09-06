-- name: file_binding_target_artifact :one
SELECT a.id::text, a.version, p.id::text, p.ref, a.lifecycle_state, a.scan_state
FROM control_plane.artifacts a
JOIN control_plane.projects p ON p.id = a.project_id
WHERE a.organization_id = @organization_id::uuid AND a.ref = @artifact_ref
  AND (@authority_project_id = '' OR a.project_id = @authority_project_id::uuid)
  AND a.lifecycle_state <> 'PURGED'
