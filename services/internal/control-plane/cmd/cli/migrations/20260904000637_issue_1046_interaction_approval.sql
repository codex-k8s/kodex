-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.interaction_deliveries
    ADD COLUMN approval_gate_id uuid UNIQUE REFERENCES control_plane.owner_gates(id),
    ADD COLUMN connection_version bigint NOT NULL DEFAULT 0 CHECK (connection_version >= 0),
    ADD COLUMN definition_version text NOT NULL DEFAULT '',
    ADD COLUMN execution_max_attempts integer NOT NULL DEFAULT 1 CHECK (execution_max_attempts BETWEEN 1 AND 10),
    ADD COLUMN definition_digest text NOT NULL DEFAULT '' CHECK (definition_digest = '' OR definition_digest ~ '^[a-f0-9]{64}$'),
    DROP CONSTRAINT interaction_deliveries_state_check,
    ADD CONSTRAINT interaction_deliveries_state_check CHECK
        (state IN ('WAITING_APPROVAL','DUE','CLAIMED','SUCCEEDED','FAILED','CANCELLED','UNKNOWN_OUTCOME'));

-- Прежние неподтверждённые эффекты не получают вымышленного approval.
-- Их авторитетное чтение сохраняется; новая terminal delivery создаёт gate.
UPDATE control_plane.interaction_deliveries
SET state='CANCELLED', safe_error_code='INTERACTION_APPROVAL_REQUIRED',
    version=version+1, updated_at=clock_timestamp(), completed_at=clock_timestamp()
WHERE capability_key IN ('mattermost.notifications','mattermost.result_mirror')
  AND state IN ('DUE','FAILED');

ALTER TABLE control_plane.interaction_deliveries
    ADD CONSTRAINT interaction_delivery_approval_kind CHECK
        (approval_gate_id IS NULL OR capability_key IN ('mattermost.notifications','mattermost.result_mirror')),
    ADD CONSTRAINT interaction_delivery_waiting_approval CHECK
        (state <> 'WAITING_APPROVAL' OR (approval_gate_id IS NOT NULL AND connection_version > 0 AND definition_digest <> ''));

CREATE INDEX interaction_deliveries_waiting_approval
    ON control_plane.interaction_deliveries(organization_id, available_at, id)
    WHERE state='WAITING_APPROVAL';

RESET ROLE;
