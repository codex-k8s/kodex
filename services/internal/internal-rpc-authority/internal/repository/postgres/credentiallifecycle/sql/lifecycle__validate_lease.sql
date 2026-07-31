-- name: lifecycle__validate_lease :one
SELECT EXISTS (
    SELECT 1
    FROM internal_rpc_authority.database_credential_reconciler_leases
    WHERE lease_name = 'database-credential-reconciler'
      AND holder_id = @holder_id
      AND fencing_token = @fencing_token
      AND lease_until > clock_timestamp()
    FOR UPDATE
);
