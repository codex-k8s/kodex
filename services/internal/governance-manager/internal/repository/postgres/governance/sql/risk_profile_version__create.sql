-- name: risk_profile_version__create :exec
INSERT INTO governance_manager_risk_profile_versions (
    risk_profile_id, profile_version, status, content_digest, created_at, activated_at
) VALUES (
    @risk_profile_id, @profile_version, @status, @content_digest, @created_at, @activated_at
);
