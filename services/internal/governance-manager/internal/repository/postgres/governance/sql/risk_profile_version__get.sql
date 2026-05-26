-- name: risk_profile_version__get :one
SELECT
    risk_profile_id, profile_version, status, content_digest, created_at, activated_at
FROM governance_manager_risk_profile_versions
WHERE risk_profile_id = @risk_profile_id
  AND profile_version = @profile_version;
