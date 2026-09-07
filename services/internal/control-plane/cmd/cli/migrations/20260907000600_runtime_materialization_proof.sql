-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.runtime_leases
    ADD COLUMN materialization_operation text,
    ADD COLUMN materialization_request_digest text,
    ADD CONSTRAINT runtime_materialization_binding_shape CHECK (
        (materialization_operation IS NULL AND materialization_request_digest IS NULL)
        OR (materialization_operation IS NOT NULL AND materialization_request_digest IS NOT NULL
            AND materialization_operation IN (
                'platform.runtime.credentials.materialize',
                'platform.runtime.credentials.system-assistant.materialize')
            AND materialization_request_digest ~ '^[a-f0-9]{64}$')
    );
CREATE UNIQUE INDEX runtime_materialization_request_identity
    ON control_plane.runtime_leases
    (organization_id, materialization_operation, materialization_request_digest)
    WHERE materialization_request_digest IS NOT NULL;
RESET ROLE;

-- +goose Down
-- Намеренно forward-only: отзыв выполняется закрытием lease, а не откатом схемы.
