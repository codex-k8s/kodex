-- name: secret_draft_publishing_active :one
SELECT EXISTS(SELECT 1 FROM control_plane.runtime_secret_drafts WHERE secret_id=@secret_id::uuid AND state='PUBLISHING');
