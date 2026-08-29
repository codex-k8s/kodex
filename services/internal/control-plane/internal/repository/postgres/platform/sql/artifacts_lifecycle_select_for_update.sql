-- name: artifacts_lifecycle_select_for_update :one
SELECT artifact.id::text,
       COALESCE(artifact.project_id::text, ''),
       COALESCE(project.ref, ''),
       artifact.version,
       artifact.lifecycle_state
FROM control_plane.artifacts artifact
LEFT JOIN control_plane.projects project ON project.id = artifact.project_id
WHERE artifact.organization_id = @organization_id::uuid
  AND artifact.ref = @artifact_ref
FOR UPDATE OF artifact;
