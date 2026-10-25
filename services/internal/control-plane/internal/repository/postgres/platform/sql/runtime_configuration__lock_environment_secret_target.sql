-- name: runtime_configuration__lock_environment_secret_target :one
SELECT environment.scope_kind, COALESCE(environment.project_id::text,''), version.ref, version.secret_descriptors
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.runtime_environment_versions version
  ON version.environment_set_id=environment.id AND version.organization_id=environment.organization_id
 AND ((@version_ref='' AND version.id=environment.current_version_id) OR version.ref=@version_ref)
WHERE environment.organization_id=@organization_id::uuid AND environment.ref=@environment_ref
  AND environment.state='ACTIVE' AND environment.scope_kind=@scope_kind
  AND environment.project_id IS NOT DISTINCT FROM NULLIF(@project_id,'')::uuid
FOR SHARE OF environment NOWAIT;
