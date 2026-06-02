-- +goose Up
CREATE INDEX agent_manager_sessions_created_by_actor_idx
    ON agent_manager_sessions (created_by_actor_ref, updated_at DESC, id);

CREATE INDEX agent_manager_runs_provider_pull_request_idx
    ON agent_manager_runs ((provider_target->>'pull_request_ref'), updated_at DESC, id)
    WHERE provider_target ? 'pull_request_ref';

-- +goose Down
DROP INDEX IF EXISTS agent_manager_runs_provider_pull_request_idx;
DROP INDEX IF EXISTS agent_manager_sessions_created_by_actor_idx;
