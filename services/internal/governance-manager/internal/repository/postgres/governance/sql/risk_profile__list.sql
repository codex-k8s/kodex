-- name: risk_profile__list :many
SELECT
    id, scope_type, scope_ref, slug, display_name, description,
    status, active_version, version, created_at, updated_at
FROM governance_manager_risk_profiles
WHERE (@scope_type::text = '' OR scope_type = @scope_type)
  AND (@scope_ref::text = '' OR scope_ref = @scope_ref)
  AND (@status::text = '' OR status = @status)
ORDER BY scope_type, scope_ref, slug, id
LIMIT @limit::integer OFFSET @offset::integer;
