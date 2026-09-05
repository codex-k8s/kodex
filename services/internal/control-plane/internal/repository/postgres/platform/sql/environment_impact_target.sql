-- name: environment_impact_target :one
SELECT environment.ref, environment.version, revision.ref, revision.digest, project.ref, project.id::text
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.projects project ON project.id = environment.project_id AND project.lifecycle = 'ACTIVE'
JOIN control_plane.runtime_environment_versions revision
  ON revision.environment_set_id = environment.id AND revision.organization_id = environment.organization_id
WHERE environment.organization_id = $1::uuid AND environment.ref = $2
  AND environment.state = 'ACTIVE'
  AND (($3 = '' AND revision.id = environment.current_version_id) OR revision.ref = $3);
