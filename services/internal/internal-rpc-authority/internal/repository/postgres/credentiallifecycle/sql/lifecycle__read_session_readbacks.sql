-- name: lifecycle__read_session_readbacks :many
SELECT
    capability,
    generation,
    lifecycle_status,
    principal,
    credential_digest_sha256,
    pod_uid,
    observed_at
FROM internal_rpc_authority.database_credential_session_readbacks
WHERE observed_at > clock_timestamp() - interval '15 minutes'
ORDER BY capability, generation, lifecycle_status, observed_at DESC;
