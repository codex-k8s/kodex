-- name: secret_draft_creator_matches :one
SELECT created_by=@actor_id::uuid FROM control_plane.runtime_secrets WHERE id=@secret_id::uuid;
