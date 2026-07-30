-- +goose Up
ALTER TABLE internal_rpc_authority.authority_restore_fences
    ADD CONSTRAINT authority_restore_fences_phase_check
    CHECK (phase IN (
        'OPEN',
        'QUIESCING',
        'PREPARED',
        'RESTORING',
        'COMPLETED',
        'FENCED_SAFE_WINDOW'
    )) NOT VALID;

ALTER TABLE internal_rpc_authority.authority_restore_fences
    VALIDATE CONSTRAINT authority_restore_fences_phase_check;

CREATE FUNCTION internal_rpc_authority.runtime_restore_fence_allows_work()
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
    SELECT NOT EXISTS (
        SELECT 1
        FROM internal_rpc_authority.authority_restore_fences AS fence
        WHERE fence.phase IN (
            'QUIESCING',
            'PREPARED',
            'RESTORING',
            'FENCED_SAFE_WINDOW'
        )
           OR (
               fence.phase = 'COMPLETED'
               AND (
                   fence.safe_window_not_before IS NULL
                   OR fence.safe_window_not_before > clock_timestamp()
               )
           )
    );
$function$;

ALTER FUNCTION internal_rpc_authority.runtime_restore_fence_allows_work()
    OWNER TO internal_rpc_authority_readback_owner;

REVOKE ALL ON FUNCTION
    internal_rpc_authority.runtime_restore_fence_allows_work()
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
    internal_rpc_authority.runtime_restore_fence_allows_work()
    TO internal_rpc_authority_issuer, internal_rpc_authority_verifier;

-- +goose Down
REVOKE ALL ON FUNCTION
    internal_rpc_authority.runtime_restore_fence_allows_work()
    FROM internal_rpc_authority_issuer, internal_rpc_authority_verifier;
DROP FUNCTION internal_rpc_authority.runtime_restore_fence_allows_work();
ALTER TABLE internal_rpc_authority.authority_restore_fences
    DROP CONSTRAINT authority_restore_fences_phase_check;
