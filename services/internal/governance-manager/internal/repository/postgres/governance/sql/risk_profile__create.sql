-- name: risk_profile__create :exec
INSERT INTO governance_manager_risk_profiles (
    id, scope_type, scope_ref, slug, display_name, description,
    status, active_version, version, created_at, updated_at
) VALUES (
    @id, @scope_type, @scope_ref, @slug, @display_name::jsonb, @description::jsonb,
    @status, @active_version, @version, @created_at, @updated_at
);
