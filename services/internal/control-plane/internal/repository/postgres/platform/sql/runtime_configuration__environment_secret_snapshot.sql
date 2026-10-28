-- name: runtime_configuration__environment_secret_snapshot :one
SELECT environment.scope_kind, COALESCE(environment.project_id::text,''), version.ref, version.secret_descriptors
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.runtime_environment_versions version
  ON version.environment_set_id=environment.id AND version.organization_id=environment.organization_id
WHERE environment.organization_id=@organization_id::uuid AND environment.ref=@environment_ref
  AND environment.state='ACTIVE'
  AND ((@version_id='' AND version.ref=@version_ref)
    OR (@version_ref='' AND version.id=NULLIF(@version_id,'')::uuid));
