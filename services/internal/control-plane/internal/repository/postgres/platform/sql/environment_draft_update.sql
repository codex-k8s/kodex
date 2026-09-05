-- name: environment_draft_update :execrows
UPDATE control_plane.runtime_environment_drafts
SET specification = @specification::jsonb, state = @state, validation_digest = @validation_digest,
    diagnostics = @diagnostics::jsonb, published_environment_ref = @published_ref, version = version + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid AND ref = @ref AND version = @version
  AND state NOT IN ('PUBLISHED', 'DISCARDED');
