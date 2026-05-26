-- name: risk_profile_version__supersede :exec
UPDATE governance_manager_risk_profile_versions
SET status = 'superseded'
WHERE risk_profile_id = @risk_profile_id
  AND profile_version <> @profile_version
  AND status = 'active';
