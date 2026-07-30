-- name: lifecycle__acquire_lease :one
INSERT INTO internal_rpc_authority.database_credential_reconciler_leases (
    lease_name,
    holder_id,
    fencing_token,
    lease_until,
    updated_at
)
VALUES (
    'database-credential-reconciler',
    @holder_id,
    1,
    clock_timestamp() + make_interval(secs => @lease_duration_seconds),
    clock_timestamp()
)
ON CONFLICT (lease_name) DO UPDATE
SET holder_id = EXCLUDED.holder_id,
    fencing_token = CASE
        WHEN internal_rpc_authority.database_credential_reconciler_leases.holder_id =
            EXCLUDED.holder_id
            THEN internal_rpc_authority.database_credential_reconciler_leases.fencing_token
        ELSE internal_rpc_authority.database_credential_reconciler_leases.fencing_token + 1
    END,
    lease_until = EXCLUDED.lease_until,
    updated_at = EXCLUDED.updated_at
WHERE internal_rpc_authority.database_credential_reconciler_leases.lease_until <
        clock_timestamp()
   OR internal_rpc_authority.database_credential_reconciler_leases.holder_id =
        EXCLUDED.holder_id
RETURNING fencing_token;
