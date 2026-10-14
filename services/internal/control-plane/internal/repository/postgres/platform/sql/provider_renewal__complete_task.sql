-- name: provider_renewal__complete_task :exec
UPDATE control_plane.provider_credential_renewal_tasks
SET state = 'SUCCEEDED', completed_at = clock_timestamp(), output_credential_id = @credential_id::uuid,
    output_account_version = account_version + 1, materializer_receipt_uid = @receipt_uid::uuid,
    materializer_receipt_version = @receipt_version, result_digest = @result_digest
WHERE id = @task_id::uuid AND state IN ('AUTHORIZED', 'UNKNOWN_OUTCOME');
