-- +goose Up
CREATE TABLE runtime_manager_slots (
    id uuid PRIMARY KEY,
    slot_key text NOT NULL UNIQUE,
    status text NOT NULL,
    runtime_mode text NOT NULL,
    is_prewarmed boolean NOT NULL DEFAULT false,
    fleet_scope_id uuid,
    cluster_id uuid,
    namespace_name text NOT NULL DEFAULT '',
    agent_run_id uuid,
    project_id uuid,
    repository_ids_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    runtime_profile text NOT NULL,
    fingerprint text NOT NULL DEFAULT '',
    lease_owner text NOT NULL DEFAULT '',
    lease_until timestamptz,
    last_error_code text NOT NULL DEFAULT '',
    last_error_message text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT runtime_manager_slots_status_chk
        CHECK (status IN ('prewarmed', 'reserved', 'materializing', 'ready', 'in_use', 'releasing', 'failed', 'cleanup_pending', 'cleaned')),
    CONSTRAINT runtime_manager_slots_runtime_mode_chk
        CHECK (runtime_mode IN ('code_only', 'full_env', 'read_only_production')),
    CONSTRAINT runtime_manager_slots_repository_ids_chk
        CHECK (jsonb_typeof(repository_ids_json) = 'array'),
    CONSTRAINT runtime_manager_slots_runtime_profile_chk CHECK (runtime_profile <> ''),
    CONSTRAINT runtime_manager_slots_version_chk CHECK (version > 0)
);

CREATE INDEX runtime_manager_slots_status_lease_idx
    ON runtime_manager_slots (status, lease_until);

CREATE INDEX runtime_manager_slots_project_status_idx
    ON runtime_manager_slots (project_id, status);

CREATE INDEX runtime_manager_slots_agent_run_idx
    ON runtime_manager_slots (agent_run_id);

CREATE INDEX runtime_manager_slots_runtime_profile_idx
    ON runtime_manager_slots (runtime_profile, status);

CREATE INDEX runtime_manager_slots_fleet_scope_idx
    ON runtime_manager_slots (fleet_scope_id, cluster_id, status);

CREATE TABLE runtime_manager_workspace_materializations (
    id uuid PRIMARY KEY,
    slot_id uuid NOT NULL REFERENCES runtime_manager_slots(id),
    status text NOT NULL,
    policy_digest text NOT NULL,
    sources_json jsonb NOT NULL,
    fingerprint text NOT NULL DEFAULT '',
    started_at timestamptz,
    finished_at timestamptz,
    last_error_code text NOT NULL DEFAULT '',
    last_error_message text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT runtime_manager_workspace_materializations_status_chk
        CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    CONSTRAINT runtime_manager_workspace_materializations_policy_digest_chk CHECK (policy_digest <> ''),
    CONSTRAINT runtime_manager_workspace_materializations_sources_chk
        CHECK (jsonb_typeof(sources_json) = 'array'),
    CONSTRAINT runtime_manager_workspace_materializations_finished_chk
        CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at),
    CONSTRAINT runtime_manager_workspace_materializations_version_chk CHECK (version > 0)
);

CREATE INDEX runtime_manager_workspace_materializations_slot_status_idx
    ON runtime_manager_workspace_materializations (slot_id, status);

CREATE INDEX runtime_manager_workspace_materializations_fingerprint_idx
    ON runtime_manager_workspace_materializations (fingerprint);

CREATE TABLE runtime_manager_command_results (
    key text PRIMARY KEY,
    command_id uuid,
    idempotency_key text NOT NULL DEFAULT '',
    actor_type text NOT NULL,
    actor_id text NOT NULL,
    operation text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    result_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT runtime_manager_command_results_identity_chk
        CHECK (command_id IS NOT NULL OR idempotency_key <> ''),
    CONSTRAINT runtime_manager_command_results_actor_chk CHECK (actor_type <> '' AND actor_id <> ''),
    CONSTRAINT runtime_manager_command_results_operation_chk CHECK (operation <> ''),
    CONSTRAINT runtime_manager_command_results_aggregate_type_chk CHECK (aggregate_type <> ''),
    CONSTRAINT runtime_manager_command_results_payload_chk CHECK (jsonb_typeof(result_payload) = 'object')
);

CREATE UNIQUE INDEX runtime_manager_command_results_command_id_uidx
    ON runtime_manager_command_results (command_id)
    WHERE command_id IS NOT NULL;

CREATE UNIQUE INDEX runtime_manager_command_results_idempotency_uidx
    ON runtime_manager_command_results (operation, actor_type, actor_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE INDEX runtime_manager_command_results_aggregate_idx
    ON runtime_manager_command_results (aggregate_type, aggregate_id, created_at);

CREATE TABLE runtime_manager_jobs (
    id uuid PRIMARY KEY,
    command_id text NOT NULL,
    job_type text NOT NULL,
    status text NOT NULL,
    priority text NOT NULL,
    job_input_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    lease_owner text NOT NULL DEFAULT '',
    lease_token_hash text NOT NULL DEFAULT '',
    lease_until timestamptz,
    claim_attempt bigint NOT NULL DEFAULT 0,
    slot_id uuid REFERENCES runtime_manager_slots(id),
    agent_run_id uuid,
    project_id uuid,
    repository_id uuid,
    release_line_id uuid,
    package_installation_id uuid,
    fleet_scope_id uuid,
    cluster_id uuid,
    requested_by uuid,
    created_at timestamptz NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    next_action text NOT NULL DEFAULT '',
    last_error_code text NOT NULL DEFAULT '',
    last_error_message text NOT NULL DEFAULT '',
    short_log_tail text NOT NULL DEFAULT '',
    full_log_ref text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT runtime_manager_jobs_command_id_chk CHECK (command_id <> ''),
    CONSTRAINT runtime_manager_jobs_type_chk
        CHECK (job_type IN ('mirror', 'build', 'deploy', 'cleanup', 'health_check', 'housekeeping', 'workspace_materialization')),
    CONSTRAINT runtime_manager_jobs_status_chk
        CHECK (status IN ('pending', 'claimed', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out')),
    CONSTRAINT runtime_manager_jobs_priority_chk
        CHECK (priority IN ('low', 'normal', 'high', 'blocking')),
    CONSTRAINT runtime_manager_jobs_input_chk CHECK (jsonb_typeof(job_input_json) = 'object'),
    CONSTRAINT runtime_manager_jobs_claim_attempt_chk CHECK (claim_attempt >= 0),
    CONSTRAINT runtime_manager_jobs_finished_chk
        CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at),
    CONSTRAINT runtime_manager_jobs_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX runtime_manager_jobs_command_uidx
    ON runtime_manager_jobs (command_id);

CREATE INDEX runtime_manager_jobs_status_claim_idx
    ON runtime_manager_jobs (status, lease_until, priority, created_at);

CREATE INDEX runtime_manager_jobs_slot_status_idx
    ON runtime_manager_jobs (slot_id, status);

CREATE INDEX runtime_manager_jobs_project_status_idx
    ON runtime_manager_jobs (project_id, status);

CREATE INDEX runtime_manager_jobs_agent_run_idx
    ON runtime_manager_jobs (agent_run_id);

CREATE INDEX runtime_manager_jobs_release_line_status_idx
    ON runtime_manager_jobs (release_line_id, status);

CREATE INDEX runtime_manager_jobs_fleet_scope_status_idx
    ON runtime_manager_jobs (fleet_scope_id, cluster_id, status);

CREATE TABLE runtime_manager_job_steps (
    id uuid PRIMARY KEY,
    job_id uuid NOT NULL REFERENCES runtime_manager_jobs(id) ON DELETE CASCADE,
    step_key text NOT NULL,
    status text NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    short_log_tail text NOT NULL DEFAULT '',
    external_ref text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (job_id, step_key),
    CONSTRAINT runtime_manager_job_steps_step_key_chk CHECK (step_key <> ''),
    CONSTRAINT runtime_manager_job_steps_status_chk
        CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'skipped')),
    CONSTRAINT runtime_manager_job_steps_finished_chk
        CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at),
    CONSTRAINT runtime_manager_job_steps_version_chk CHECK (version > 0)
);

CREATE INDEX runtime_manager_job_steps_job_status_idx
    ON runtime_manager_job_steps (job_id, status);

CREATE TABLE runtime_manager_artifact_refs (
    id uuid PRIMARY KEY,
    job_id uuid REFERENCES runtime_manager_jobs(id) ON DELETE CASCADE,
    slot_id uuid REFERENCES runtime_manager_slots(id) ON DELETE CASCADE,
    artifact_type text NOT NULL,
    external_ref text NOT NULL,
    digest text NOT NULL DEFAULT '',
    metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT runtime_manager_artifact_refs_parent_chk CHECK (job_id IS NOT NULL OR slot_id IS NOT NULL),
    CONSTRAINT runtime_manager_artifact_refs_type_chk
        CHECK (artifact_type IN ('image_ref', 'kubernetes_job', 'namespace', 'deployment', 'log_ref', 'manifest_ref')),
    CONSTRAINT runtime_manager_artifact_refs_external_ref_chk CHECK (external_ref <> ''),
    CONSTRAINT runtime_manager_artifact_refs_metadata_chk CHECK (jsonb_typeof(metadata_json) = 'object')
);

CREATE INDEX runtime_manager_artifact_refs_job_idx
    ON runtime_manager_artifact_refs (job_id);

CREATE INDEX runtime_manager_artifact_refs_slot_idx
    ON runtime_manager_artifact_refs (slot_id);

CREATE INDEX runtime_manager_artifact_refs_type_ref_idx
    ON runtime_manager_artifact_refs (artifact_type, external_ref);

CREATE TABLE runtime_manager_cleanup_policies (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_id text NOT NULL DEFAULT '',
    ttl_seconds bigint NOT NULL,
    failed_ttl_seconds bigint NOT NULL,
    keep_short_log_tail boolean NOT NULL DEFAULT true,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT runtime_manager_cleanup_policies_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository', 'runtime_profile')),
    CONSTRAINT runtime_manager_cleanup_policies_scope_id_chk
        CHECK ((scope_type = 'platform' AND scope_id = '') OR (scope_type <> 'platform' AND scope_id <> '')),
    CONSTRAINT runtime_manager_cleanup_policies_ttl_chk CHECK (ttl_seconds > 0),
    CONSTRAINT runtime_manager_cleanup_policies_failed_ttl_chk CHECK (failed_ttl_seconds > 0),
    CONSTRAINT runtime_manager_cleanup_policies_status_chk
        CHECK (status IN ('active', 'disabled', 'superseded')),
    CONSTRAINT runtime_manager_cleanup_policies_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX runtime_manager_cleanup_policies_active_uidx
    ON runtime_manager_cleanup_policies (scope_type, scope_id)
    WHERE status = 'active';

CREATE INDEX runtime_manager_cleanup_policies_scope_status_idx
    ON runtime_manager_cleanup_policies (scope_type, scope_id, status);

CREATE TABLE runtime_manager_prewarm_pools (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_id text NOT NULL DEFAULT '',
    runtime_profile text NOT NULL,
    fleet_scope_id uuid,
    target_size bigint NOT NULL,
    status text NOT NULL,
    last_capacity_status text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT runtime_manager_prewarm_pools_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository')),
    CONSTRAINT runtime_manager_prewarm_pools_scope_id_chk
        CHECK ((scope_type = 'platform' AND scope_id = '') OR (scope_type <> 'platform' AND scope_id <> '')),
    CONSTRAINT runtime_manager_prewarm_pools_runtime_profile_chk CHECK (runtime_profile <> ''),
    CONSTRAINT runtime_manager_prewarm_pools_target_size_chk CHECK (target_size >= 0),
    CONSTRAINT runtime_manager_prewarm_pools_status_chk
        CHECK (status IN ('active', 'paused', 'disabled')),
    CONSTRAINT runtime_manager_prewarm_pools_capacity_status_chk
        CHECK (last_capacity_status IN ('', 'ok', 'degraded', 'insufficient')),
    CONSTRAINT runtime_manager_prewarm_pools_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX runtime_manager_prewarm_pools_active_uidx
    ON runtime_manager_prewarm_pools (
        scope_type,
        scope_id,
        runtime_profile,
        COALESCE(fleet_scope_id, '00000000-0000-0000-0000-000000000000'::uuid)
    )
    WHERE status = 'active';

CREATE INDEX runtime_manager_prewarm_pools_scope_status_idx
    ON runtime_manager_prewarm_pools (scope_type, scope_id, runtime_profile, status);

CREATE TABLE runtime_manager_outbox_events (
    id uuid PRIMARY KEY,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    payload jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    published_at timestamptz,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT '1970-01-01 00:00:00+00'::timestamptz,
    locked_until timestamptz,
    failed_permanently_at timestamptz,
    failure_kind text NOT NULL DEFAULT '',
    last_error text NOT NULL DEFAULT '',
    CONSTRAINT runtime_manager_outbox_events_event_type_chk CHECK (event_type <> ''),
    CONSTRAINT runtime_manager_outbox_events_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT runtime_manager_outbox_events_aggregate_type_chk CHECK (aggregate_type <> ''),
    CONSTRAINT runtime_manager_outbox_events_payload_chk CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT runtime_manager_outbox_events_attempt_count_chk CHECK (attempt_count >= 0),
    CONSTRAINT runtime_manager_outbox_events_failure_kind_chk
        CHECK (failure_kind IN ('', 'transient', 'permanent'))
);

CREATE INDEX runtime_manager_outbox_events_unpublished_idx
    ON runtime_manager_outbox_events (occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX runtime_manager_outbox_events_claim_idx
    ON runtime_manager_outbox_events (next_attempt_at, occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX runtime_manager_outbox_events_lock_idx
    ON runtime_manager_outbox_events (locked_until)
    WHERE published_at IS NULL AND locked_until IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS runtime_manager_outbox_events;
DROP TABLE IF EXISTS runtime_manager_prewarm_pools;
DROP TABLE IF EXISTS runtime_manager_cleanup_policies;
DROP TABLE IF EXISTS runtime_manager_artifact_refs;
DROP TABLE IF EXISTS runtime_manager_job_steps;
DROP TABLE IF EXISTS runtime_manager_jobs;
DROP TABLE IF EXISTS runtime_manager_command_results;
DROP TABLE IF EXISTS runtime_manager_workspace_materializations;
DROP TABLE IF EXISTS runtime_manager_slots;
