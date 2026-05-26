-- name: risk_profile_version__activate :exec
UPDATE governance_manager_risk_profile_versions
SET status = @status,
    activated_at = @activated_at
WHERE risk_profile_id = @risk_profile_id
  AND profile_version = @profile_version;
