-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.integration_invocations
    ADD COLUMN claimed_lease_ref text NOT NULL DEFAULT '',
    ADD COLUMN claimed_fence_digest text NOT NULL DEFAULT '',
    ADD COLUMN claimed_workload text NOT NULL DEFAULT ''
    CHECK (claimed_workload IN ('', 'integration-gateway', 'interaction-gateway'));

UPDATE control_plane.integration_invocations SET claimed_workload='integration-gateway',
    claimed_lease_ref=COALESCE(lease_ref,''),claimed_fence_digest=COALESCE(effect_fence_digest,'')
WHERE generation>0;
RESET ROLE;

-- +goose Down
SELECT 1;
