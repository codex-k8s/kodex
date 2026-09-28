-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.integration_grants
    ADD COLUMN approval_scope_paths text[] NOT NULL DEFAULT '{}'::text[],
    ADD CONSTRAINT integration_grants_approval_scope_paths_check CHECK (
        cardinality(approval_scope_paths) <= 16
        AND octet_length(array_to_string(approval_scope_paths, E'\x1f')) <= 4096
        AND ((approval_policy = 'HUMAN_SCOPED') = (cardinality(approval_scope_paths) > 0))
    );

RESET ROLE;
