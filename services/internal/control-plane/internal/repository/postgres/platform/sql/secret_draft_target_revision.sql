-- name: secret_draft_target_revision :one
SELECT GREATEST(COALESCE((SELECT max(revision) FROM control_plane.runtime_secret_revisions WHERE secret_id=@secret_id::uuid),0),
COALESCE((SELECT max(o.target_revision) FROM control_plane.runtime_secret_draft_operations o
JOIN control_plane.runtime_secret_drafts d ON d.id=o.draft_id WHERE d.secret_id=@secret_id::uuid),0),
COALESCE((SELECT max(target_revision) FROM control_plane.runtime_secret_operations WHERE secret_id=@secret_id::uuid),0))+1;
