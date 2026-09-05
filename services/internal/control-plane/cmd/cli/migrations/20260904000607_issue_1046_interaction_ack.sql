-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.interaction_message_receipts
    ADD COLUMN external_team_ref text NOT NULL DEFAULT '',
    ADD COLUMN external_channel_ref text NOT NULL DEFAULT '',
    ADD COLUMN external_root_post_ref text NOT NULL DEFAULT '';

ALTER TABLE control_plane.interaction_deliveries
    ADD COLUMN acceptance_receipt_id uuid UNIQUE REFERENCES control_plane.interaction_message_receipts(id),
    ADD COLUMN external_team_ref text NOT NULL DEFAULT '',
    ADD COLUMN external_channel_ref text NOT NULL DEFAULT '',
    ADD COLUMN target_root_post_ref text NOT NULL DEFAULT '',
    DROP CONSTRAINT interaction_deliveries_capability_key_check,
    ADD CONSTRAINT interaction_deliveries_capability_key_check CHECK
        (capability_key IN ('mattermost.notifications','mattermost.result_mirror','mattermost.gate_decisions','mattermost.acknowledgements')),
    ADD CONSTRAINT interaction_ack_exact_target CHECK
        ((capability_key='mattermost.acknowledgements' AND acceptance_receipt_id IS NOT NULL
          AND external_team_ref<>'' AND external_channel_ref<>'' AND target_root_post_ref<>'')
         OR (capability_key<>'mattermost.acknowledgements' AND acceptance_receipt_id IS NULL));
DROP INDEX control_plane.interaction_deliveries_effect;
CREATE UNIQUE INDEX interaction_deliveries_effect ON control_plane.interaction_deliveries
    (connection_id,capability_key,root_run_id,COALESCE(gate_id,'00000000-0000-0000-0000-000000000000'::uuid))
    WHERE acceptance_receipt_id IS NULL;
RESET ROLE;
