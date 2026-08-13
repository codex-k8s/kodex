-- +goose Up
RESET ROLE;
SET ROLE internal_rpc_authority_owner;

-- The owner of the SECURITY DEFINER consume function must be able to acquire
-- the intent row lock under FORCE RLS before it commits a readback receipt.
CREATE POLICY authority_readback_intents_owner_lock
    ON internal_rpc_authority.authority_readback_intents
    FOR UPDATE
    TO internal_rpc_authority_readback_owner
    USING (true)
    WITH CHECK (true);

RESET ROLE;

-- +goose Down
-- Forward-only: rollback is performed by a separate compensating migration.
