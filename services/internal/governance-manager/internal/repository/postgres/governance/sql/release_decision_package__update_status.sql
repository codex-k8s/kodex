-- name: release_decision_package__update_status :exec
UPDATE governance_manager_release_decision_packages
SET
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
