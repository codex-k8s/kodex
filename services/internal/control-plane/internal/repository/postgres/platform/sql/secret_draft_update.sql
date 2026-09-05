-- name: secret_draft_update :one
UPDATE control_plane.runtime_secret_drafts SET state=@state,version=version+1,
encrypted_descriptor=COALESCE(@encrypted::jsonb,encrypted_descriptor),published_revision=@published_revision,updated_at=clock_timestamp()
WHERE id=@draft_id::uuid AND version=@version
RETURNING version,updated_at;
