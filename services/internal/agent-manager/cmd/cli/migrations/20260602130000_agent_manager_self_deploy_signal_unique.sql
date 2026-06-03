-- +goose Up
DROP INDEX IF EXISTS agent_manager_self_deploy_plans_signal_idx;

CREATE UNIQUE INDEX agent_manager_self_deploy_plans_signal_idx
    ON agent_manager_self_deploy_plans (provider_signal_ref)
    WHERE provider_signal_ref <> '';

-- +goose Down
DROP INDEX IF EXISTS agent_manager_self_deploy_plans_signal_idx;

CREATE INDEX agent_manager_self_deploy_plans_signal_idx
    ON agent_manager_self_deploy_plans (provider_signal_ref, updated_at DESC, id)
    WHERE provider_signal_ref <> '';
