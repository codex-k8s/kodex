-- +goose Up
CREATE UNIQUE INDEX interaction_hub_responses_channel_callback_source_uidx
    ON interaction_hub_responses (source_kind, source_ref)
    WHERE source_kind = 'channel_callback' AND source_ref <> '';

-- +goose Down
DROP INDEX IF EXISTS interaction_hub_responses_channel_callback_source_uidx;
