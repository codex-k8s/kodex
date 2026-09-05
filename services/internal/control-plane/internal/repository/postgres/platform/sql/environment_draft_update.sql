-- name: environment_draft_update :one
UPDATE control_plane.runtime_environment_drafts
SET specification = @specification::jsonb, state = @state, validation_digest = @validation_digest,
    diagnostics = @diagnostics::jsonb, published_environment_ref = @published_ref, version = version + 1, updated_at = clock_timestamp(),
    saved_at = CASE WHEN @save_content::boolean THEN clock_timestamp() ELSE saved_at END
WHERE organization_id = @organization_id::uuid AND ref = @ref AND version = @version
  AND state NOT IN ('PUBLISHED', 'DISCARDED')
RETURNING saved_at;
