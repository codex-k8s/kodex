-- name: context_reservations__delete_expired :exec
DELETE FROM internal_rpc_authority.authority_replay_reservations
WHERE expires_at < @delete_before
  AND accepted_at < @delete_before;
