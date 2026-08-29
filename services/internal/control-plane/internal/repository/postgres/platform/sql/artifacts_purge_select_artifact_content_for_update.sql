-- name: artifacts_purge_select_artifact_content_for_update :one
SELECT artifact.id::text,
       COALESCE(artifact.project_id::text, ''),
       COALESCE(project.ref, ''),
       artifact.version,
       artifact.lifecycle_state,
       content.object_key,
       content.object_version
FROM control_plane.artifacts artifact
LEFT JOIN control_plane.projects project ON project.id = artifact.project_id
LEFT JOIN control_plane.artifact_content content ON content.artifact_id = artifact.id
WHERE artifact.organization_id = @organization_id::uuid
  AND artifact.ref = @artifact_ref
FOR UPDATE OF artifact;
