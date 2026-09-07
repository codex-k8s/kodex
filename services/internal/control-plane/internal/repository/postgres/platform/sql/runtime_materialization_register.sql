-- name: runtime_materialization_register :exec
UPDATE control_plane.runtime_leases
SET materialization_operation = @operation,
    materialization_request_digest = @request_digest
WHERE organization_id = @organization_id::uuid
  AND ref = @lease_ref
  AND state = 'CLAIMED'
  AND expires_at > clock_timestamp()
  AND materialization_operation IS NULL
  AND materialization_request_digest IS NULL;
