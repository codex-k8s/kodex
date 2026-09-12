-- +goose Up
SET ROLE control_plane_owner;
CREATE UNIQUE INDEX runtime_materialization_global_request_identity
    ON control_plane.runtime_leases
    (materialization_operation, materialization_request_digest)
    WHERE materialization_request_digest IS NOT NULL;
RESET ROLE;

-- +goose Down
-- Намеренно forward-only: tenant materialization определяется точным lease,
-- а отзыв выполняется его закрытием, а не откатом схемы.
