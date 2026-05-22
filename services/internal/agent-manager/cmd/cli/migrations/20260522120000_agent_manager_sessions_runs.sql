-- +goose Up
ALTER TABLE agent_manager_command_results
    DROP CONSTRAINT agent_manager_command_results_aggregate_type_chk;

ALTER TABLE agent_manager_command_results
    ADD CONSTRAINT agent_manager_command_results_aggregate_type_chk
        CHECK (aggregate_type IN (
            'flow',
            'flow_version',
            'role_profile',
            'prompt_template',
            'prompt_template_version',
            'session',
            'run',
            'session_state_snapshot'
        ));

CREATE TABLE agent_manager_sessions (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    provider_work_item_ref text NOT NULL DEFAULT '',
    flow_version_id uuid REFERENCES agent_manager_flow_versions(id),
    current_stage_id uuid REFERENCES agent_manager_stages(id),
    latest_state_snapshot_id uuid,
    status text NOT NULL,
    created_by_actor_ref text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_sessions_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository')),
    CONSTRAINT agent_manager_sessions_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT agent_manager_sessions_status_chk
        CHECK (status IN ('open', 'waiting', 'completed', 'failed', 'cancelled')),
    CONSTRAINT agent_manager_sessions_created_by_chk CHECK (created_by_actor_ref <> ''),
    CONSTRAINT agent_manager_sessions_version_chk CHECK (version > 0)
);

CREATE INDEX agent_manager_sessions_scope_status_idx
    ON agent_manager_sessions (scope_type, scope_ref, status, updated_at DESC, id);

CREATE INDEX agent_manager_sessions_provider_work_item_idx
    ON agent_manager_sessions (provider_work_item_ref)
    WHERE provider_work_item_ref <> '';

CREATE TABLE agent_manager_runs (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES agent_manager_sessions(id),
    flow_version_id uuid REFERENCES agent_manager_flow_versions(id),
    stage_id uuid REFERENCES agent_manager_stages(id),
    role_profile_id uuid NOT NULL REFERENCES agent_manager_role_profiles(id),
    role_profile_version bigint NOT NULL,
    role_profile_digest text NOT NULL,
    prompt_template_version_id uuid NOT NULL REFERENCES agent_manager_prompt_template_versions(id),
    prompt_template_digest text NOT NULL,
    runtime_context jsonb NOT NULL DEFAULT '{}'::jsonb,
    provider_target jsonb NOT NULL DEFAULT '{}'::jsonb,
    guidance_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    status text NOT NULL,
    result_summary text NOT NULL DEFAULT '',
    failure_code text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_runs_role_version_chk CHECK (role_profile_version > 0),
    CONSTRAINT agent_manager_runs_role_digest_chk CHECK (role_profile_digest <> ''),
    CONSTRAINT agent_manager_runs_prompt_digest_chk CHECK (prompt_template_digest <> ''),
    CONSTRAINT agent_manager_runs_runtime_context_chk CHECK (jsonb_typeof(runtime_context) = 'object'),
    CONSTRAINT agent_manager_runs_provider_target_chk CHECK (jsonb_typeof(provider_target) = 'object'),
    CONSTRAINT agent_manager_runs_guidance_refs_chk CHECK (jsonb_typeof(guidance_refs) = 'array'),
    CONSTRAINT agent_manager_runs_status_chk
        CHECK (status IN ('requested', 'starting', 'running', 'waiting', 'completed', 'failed', 'cancelled')),
    CONSTRAINT agent_manager_runs_version_chk CHECK (version > 0)
);

CREATE INDEX agent_manager_runs_session_status_idx
    ON agent_manager_runs (session_id, status, updated_at DESC, id);

CREATE INDEX agent_manager_runs_role_status_idx
    ON agent_manager_runs (role_profile_id, status, updated_at DESC, id);

CREATE INDEX agent_manager_runs_provider_work_item_idx
    ON agent_manager_runs ((provider_target->>'work_item_ref'), updated_at DESC, id)
    WHERE provider_target ? 'work_item_ref';

CREATE TABLE agent_manager_session_state_snapshots (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES agent_manager_sessions(id),
    run_id uuid REFERENCES agent_manager_runs(id),
    snapshot_kind text NOT NULL,
    turn_index bigint,
    object_uri text NOT NULL,
    object_digest text NOT NULL,
    object_size_bytes bigint,
    captured_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT agent_manager_session_state_snapshots_kind_chk
        CHECK (snapshot_kind IN ('turn_checkpoint', 'run_completion', 'manual_checkpoint', 'recovery_checkpoint')),
    CONSTRAINT agent_manager_session_state_snapshots_turn_chk CHECK (turn_index IS NULL OR turn_index >= 0),
    CONSTRAINT agent_manager_session_state_snapshots_object_uri_chk CHECK (object_uri <> ''),
    CONSTRAINT agent_manager_session_state_snapshots_object_digest_chk CHECK (object_digest <> ''),
    CONSTRAINT agent_manager_session_state_snapshots_object_size_chk
        CHECK (object_size_bytes IS NULL OR object_size_bytes >= 0)
);

ALTER TABLE agent_manager_sessions
    ADD CONSTRAINT agent_manager_sessions_latest_snapshot_fk
        FOREIGN KEY (latest_state_snapshot_id)
        REFERENCES agent_manager_session_state_snapshots(id);

CREATE INDEX agent_manager_session_state_snapshots_session_created_idx
    ON agent_manager_session_state_snapshots (session_id, created_at DESC, id);

CREATE INDEX agent_manager_session_state_snapshots_run_created_idx
    ON agent_manager_session_state_snapshots (run_id, created_at DESC, id)
    WHERE run_id IS NOT NULL;

-- +goose Down
ALTER TABLE agent_manager_sessions
    DROP CONSTRAINT IF EXISTS agent_manager_sessions_latest_snapshot_fk;

DROP TABLE IF EXISTS agent_manager_session_state_snapshots;
DROP TABLE IF EXISTS agent_manager_runs;
DROP TABLE IF EXISTS agent_manager_sessions;

ALTER TABLE agent_manager_command_results
    DROP CONSTRAINT agent_manager_command_results_aggregate_type_chk;

ALTER TABLE agent_manager_command_results
    ADD CONSTRAINT agent_manager_command_results_aggregate_type_chk
        CHECK (aggregate_type IN (
            'flow',
            'flow_version',
            'role_profile',
            'prompt_template',
            'prompt_template_version'
        ));
