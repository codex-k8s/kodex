-- +goose Up
ALTER TABLE interaction_hub_responses
    DROP CONSTRAINT interaction_hub_responses_action_chk;

ALTER TABLE interaction_hub_responses
    ADD CONSTRAINT interaction_hub_responses_action_chk
        CHECK (response_action IN ('answer', 'approve', 'reject', 'request_changes', 'defer', 'acknowledge', 'custom'));

-- +goose Down
ALTER TABLE interaction_hub_responses
    DROP CONSTRAINT interaction_hub_responses_action_chk;

ALTER TABLE interaction_hub_responses
    ADD CONSTRAINT interaction_hub_responses_action_chk
        CHECK (response_action IN ('answer', 'approve', 'reject', 'defer', 'acknowledge', 'custom'));
