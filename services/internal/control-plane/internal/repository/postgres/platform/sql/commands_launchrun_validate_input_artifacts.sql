-- name: commands_launchrun_validate_input_artifacts :one
SELECT cardinality(@artifact_refs::text[]) = COUNT(DISTINCT artifact.ref)
   AND COALESCE(SUM(artifact.size_bytes), 0) <= 536870912
FROM unnest(@artifact_refs::text[]) AS requested(ref)
LEFT JOIN control_plane.artifacts AS artifact
  ON artifact.organization_id = @organization_id::uuid
 AND artifact.project_id = @project_id::uuid
 AND artifact.ref = requested.ref
 AND artifact.scan_state = 'CLEAN'
 AND artifact.lifecycle_state = 'ACTIVE'
