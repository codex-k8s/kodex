-- name: reservations__delete_expired :exec
WITH deleted_contexts AS (
    DELETE FROM internal_rpc_authority.authority_replay_reservations
    WHERE expires_at < @delete_before
      AND accepted_at < @delete_before
    RETURNING 1
)
DELETE FROM internal_rpc_authority.authority_proof_reservations
WHERE expires_at < @delete_before
  AND accepted_at < @delete_before;
