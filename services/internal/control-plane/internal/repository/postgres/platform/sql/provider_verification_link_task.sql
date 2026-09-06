-- name: provider_verification_link_task :exec
UPDATE control_plane.provider_account_verifications verification
SET model_catalog_task_id = task.id
FROM control_plane.provider_model_catalog_tasks task
WHERE task.id = @task_id::uuid AND verification.provider_account_id = task.provider_account_id
  AND verification.organization_id = task.organization_id AND verification.account_version = task.account_version
  AND verification.provider_credential_revision_id = task.provider_credential_revision_id
  AND verification.state = 'PENDING' AND verification.model_catalog_task_id IS NULL
  AND verification.deadline > clock_timestamp();
