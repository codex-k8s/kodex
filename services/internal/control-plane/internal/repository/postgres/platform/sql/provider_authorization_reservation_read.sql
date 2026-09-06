-- name: provider_authorization_reservation_read :one
SELECT ref, request_digest, original_account_version, reserved_account_version,
       preparation_state, reservation_deadline > clock_timestamp()
FROM control_plane.provider_authorization_attempts
WHERE organization_id=@organization_id::uuid AND provider_account_id=@account_id::uuid
  AND created_by=@actor_id::uuid AND method=@method AND request_key=@request_key;
