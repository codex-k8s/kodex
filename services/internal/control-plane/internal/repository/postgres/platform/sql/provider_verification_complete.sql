-- name: provider_verification_complete :exec
UPDATE control_plane.provider_account_verifications verification
SET state = CASE WHEN observation.failure = 'NONE' AND observation.source IN ('REMOTE_API', 'REMOTE_CODEX')
                      AND observation.observed_at >= verification.requested_at THEN 'VERIFIED' ELSE 'FAILED' END,
    safe_reason = CASE WHEN observation.failure = 'NONE' AND observation.source IN ('REMOTE_API', 'REMOTE_CODEX')
                           AND observation.observed_at >= verification.requested_at THEN 'CREDENTIAL_REACHABILITY_VERIFIED'
                       ELSE 'CREDENTIAL_VERIFICATION_FAILED' END,
    completed_at = clock_timestamp()
FROM control_plane.provider_model_catalog_observations observation
WHERE verification.model_catalog_task_id = @task_id::uuid AND observation.task_id = verification.model_catalog_task_id
  AND verification.state = 'PENDING' AND verification.deadline > clock_timestamp()
  AND observation.account_version = verification.account_version
  AND observation.provider_credential_revision_id = verification.provider_credential_revision_id;
