-- +goose Up
SET ROLE internal_rpc_authority_owner;

GRANT SELECT
    ON internal_rpc_authority.authority_restore_fences
    TO internal_rpc_authority_restore_controller;

RESET ROLE;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
