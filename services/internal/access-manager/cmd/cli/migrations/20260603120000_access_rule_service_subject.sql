-- +goose Up
ALTER TABLE access_rules
    DROP CONSTRAINT IF EXISTS access_rules_subject_type_check;

ALTER TABLE access_rules
    ADD CONSTRAINT access_rules_subject_type_check
    CHECK (subject_type IN (
        'user', 'group', 'organization', 'external_account',
        'service', 'agent', 'agent_role', 'flow', 'package'
    ));

-- +goose Down
ALTER TABLE access_rules
    DROP CONSTRAINT IF EXISTS access_rules_subject_type_check;

ALTER TABLE access_rules
    ADD CONSTRAINT access_rules_subject_type_check
    CHECK (subject_type IN (
        'user', 'group', 'organization', 'external_account',
        'agent', 'agent_role', 'flow', 'package'
    ));
