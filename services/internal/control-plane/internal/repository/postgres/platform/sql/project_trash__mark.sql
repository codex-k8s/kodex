-- name: project_trash__mark :one
UPDATE control_plane.projects
SET lifecycle='TRASHED', deleted_at=statement_timestamp(),
    purge_after=statement_timestamp()+interval '30 days', deleted_by=@actor_id::uuid,
    version=version+1, updated_at=statement_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@project_id::uuid
  AND lifecycle='ACTIVE' AND version=@expected_version
RETURNING id::text, ref, name, purpose, language, lifecycle, version,
          created_at, updated_at, deleted_at, purge_after
