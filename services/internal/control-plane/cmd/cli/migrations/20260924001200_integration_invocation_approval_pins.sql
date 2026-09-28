-- +goose Up
SET ROLE control_plane_owner;

-- Решение владельца должно опираться на те же пути и версию права, что
-- существовали при создании invocation, а не на позднее изменённый grant.
ALTER TABLE control_plane.integration_invocations
    ADD COLUMN grant_version bigint NOT NULL DEFAULT 0,
    ADD COLUMN approval_scope_paths text[] NOT NULL DEFAULT '{}'::text[],
    ADD CONSTRAINT integration_invocations_approval_pins_check CHECK (
        (approval_policy <> 'HUMAN_SCOPED' AND grant_version = 0
            AND cardinality(approval_scope_paths) = 0) OR
        (approval_policy = 'HUMAN_SCOPED' AND grant_version > 0
            AND cardinality(approval_scope_paths) BETWEEN 1 AND 16)
    ) NOT VALID;

ALTER TABLE control_plane.integration_invocations
    VALIDATE CONSTRAINT integration_invocations_approval_pins_check;

RESET ROLE;
