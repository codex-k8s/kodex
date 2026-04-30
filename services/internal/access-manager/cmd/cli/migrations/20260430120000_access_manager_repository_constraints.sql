-- +goose Up
CREATE TABLE access_command_results (
    key text PRIMARY KEY,
    command_id uuid,
    idempotency_key text NOT NULL DEFAULT '',
    operation text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT access_command_results_identity_check CHECK (
        command_id IS NOT NULL OR idempotency_key <> ''
    )
);

CREATE UNIQUE INDEX access_command_results_command_id_unique_idx
    ON access_command_results (command_id)
    WHERE command_id IS NOT NULL;

CREATE UNIQUE INDEX access_command_results_idempotency_key_unique_idx
    ON access_command_results (idempotency_key)
    WHERE idempotency_key <> '';

ALTER TABLE access_external_accounts
    ADD CONSTRAINT access_external_accounts_owner_scope_type_check
    CHECK (owner_scope_type IN (
        'global', 'organization', 'project', 'repository', 'user',
        'group', 'agent', 'agent_role', 'flow', 'package'
    ));

ALTER TABLE access_external_accounts
    ADD CONSTRAINT access_external_accounts_owner_scope_id_check
    CHECK (
        (owner_scope_type = 'global' AND owner_scope_id = '') OR
        (owner_scope_type <> 'global' AND owner_scope_id <> '')
    );

ALTER TABLE access_external_account_bindings
    ADD CONSTRAINT access_external_account_bindings_usage_scope_type_check
    CHECK (usage_scope_type IN (
        'organization', 'project', 'repository', 'user', 'group',
        'agent', 'agent_role', 'flow', 'stage', 'package'
    ));

ALTER TABLE access_rules
    ADD CONSTRAINT access_rules_subject_type_check
    CHECK (subject_type IN (
        'user', 'group', 'organization', 'external_account',
        'agent', 'agent_role', 'flow', 'package'
    ));

CREATE UNIQUE INDEX access_rules_identity_unique_idx
    ON access_rules (
        effect, subject_type, subject_id, action_key, resource_type,
        resource_id, scope_type, scope_id
    );

-- +goose Down
DROP INDEX IF EXISTS access_rules_identity_unique_idx;

ALTER TABLE access_rules
    DROP CONSTRAINT IF EXISTS access_rules_subject_type_check;

ALTER TABLE access_external_account_bindings
    DROP CONSTRAINT IF EXISTS access_external_account_bindings_usage_scope_type_check;

ALTER TABLE access_external_accounts
    DROP CONSTRAINT IF EXISTS access_external_accounts_owner_scope_id_check;

ALTER TABLE access_external_accounts
    DROP CONSTRAINT IF EXISTS access_external_accounts_owner_scope_type_check;

DROP TABLE IF EXISTS access_command_results;
