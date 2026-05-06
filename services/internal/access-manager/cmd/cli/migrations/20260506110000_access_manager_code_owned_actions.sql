-- +goose Up
ALTER TABLE access_rules
    DROP CONSTRAINT IF EXISTS access_rules_action_key_fkey;

-- +goose Down
ALTER TABLE access_rules
    ADD CONSTRAINT access_rules_action_key_fkey
    FOREIGN KEY (action_key) REFERENCES access_actions(key);
