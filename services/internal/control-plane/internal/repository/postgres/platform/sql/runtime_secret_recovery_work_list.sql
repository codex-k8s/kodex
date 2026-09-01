-- name: runtime_secret_recovery_work_list :many
SELECT operation.ref, operation.kind, operation.claimant_id, operation.claim_generation,
       secret.namespace, secret.ref, operation.target_revision, 'value',
       COALESCE(operation.expected_content_sha256, ''), operation.claim_lease_deadline
FROM control_plane.runtime_secret_operations operation
JOIN control_plane.runtime_secrets secret ON secret.id = operation.secret_id
WHERE operation.organization_id = @organization_id::uuid
  AND operation.state = 'CLAIMED'
  AND operation.claim_lease_deadline <= clock_timestamp()
  AND (
    @cursor_deadline::timestamptz IS NULL OR
    operation.claim_lease_deadline > @cursor_deadline::timestamptz OR
    (operation.claim_lease_deadline = @cursor_deadline::timestamptz AND operation.ref > @cursor_ref)
  )
ORDER BY operation.claim_lease_deadline, operation.ref
LIMIT @page_size;
