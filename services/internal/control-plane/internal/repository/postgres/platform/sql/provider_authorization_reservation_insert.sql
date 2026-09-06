-- name: provider_authorization_reservation_insert :exec
INSERT INTO control_plane.provider_authorization_attempts
 (ref, organization_id, provider_account_id, method, state, created_by,
  preparation_state, request_key, request_digest, original_account_version,
  reserved_account_version, reservation_deadline)
VALUES (@attempt_ref,@organization_id::uuid,@account_id::uuid,@method,'FAILED',@actor_id::uuid,
        'RESERVED',@request_key,@request_digest,@original_version,@reserved_version,
        clock_timestamp()+interval '15 minutes');
