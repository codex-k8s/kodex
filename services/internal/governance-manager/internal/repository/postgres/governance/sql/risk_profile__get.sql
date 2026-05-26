-- name: risk_profile__get :one
SELECT
    id, scope_type, scope_ref, slug, display_name, description,
    status, active_version, version, created_at, updated_at
FROM governance_manager_risk_profiles
WHERE id = @id;
