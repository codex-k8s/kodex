-- name: project_trash__restore :one
UPDATE control_plane.projects
SET lifecycle='ACTIVE', deleted_at=NULL, purge_after=NULL, deleted_by=NULL,
    version=version+1, updated_at=statement_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@project_id::uuid
  AND lifecycle='TRASHED' AND purge_after>statement_timestamp()
  AND version=@expected_version
RETURNING id::text, ref, name, purpose, language, lifecycle, version,
          created_at, updated_at, deleted_at, purge_after
