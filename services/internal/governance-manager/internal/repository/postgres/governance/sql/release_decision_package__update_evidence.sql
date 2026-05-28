-- name: release_decision_package__update_evidence :exec
UPDATE governance_manager_release_decision_packages
SET
    runtime_refs = @runtime_refs::jsonb,
    evidence_refs = @evidence_refs::jsonb,
    integration_refs = @integration_refs::jsonb,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
