-- +goose Up
ALTER TABLE agent_manager_follow_up_intents
    DROP CONSTRAINT agent_manager_follow_up_intents_status_chk;

ALTER TABLE agent_manager_follow_up_intents
    ADD CONSTRAINT agent_manager_follow_up_intents_status_chk
        CHECK (status IN ('planned', 'requested', 'created', 'updated', 'commented', 'review_signaled', 'failed', 'cancelled'));

-- +goose Down
ALTER TABLE agent_manager_follow_up_intents
    DROP CONSTRAINT agent_manager_follow_up_intents_status_chk;

ALTER TABLE agent_manager_follow_up_intents
    ADD CONSTRAINT agent_manager_follow_up_intents_status_chk
        CHECK (status IN ('planned', 'requested', 'created', 'updated', 'commented', 'failed', 'cancelled'));
