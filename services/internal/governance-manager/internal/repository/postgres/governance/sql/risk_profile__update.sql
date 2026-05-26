-- name: risk_profile__update :exec
UPDATE governance_manager_risk_profiles
SET
    status = @status,
    active_version = @active_version,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
