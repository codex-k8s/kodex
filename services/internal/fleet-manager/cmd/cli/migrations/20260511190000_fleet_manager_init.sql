-- +goose Up
CREATE TABLE fleet_manager_scopes (
    id uuid PRIMARY KEY,
    scope_key text NOT NULL UNIQUE,
    scope_type text NOT NULL,
    scope_owner_id uuid,
    owner_ref_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    display_name text NOT NULL,
    status text NOT NULL,
    is_default boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT fleet_manager_scopes_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository', 'service')),
    CONSTRAINT fleet_manager_scopes_status_chk
        CHECK (status IN ('active', 'suspended', 'draining', 'archived')),
    CONSTRAINT fleet_manager_scopes_key_chk CHECK (scope_key <> ''),
    CONSTRAINT fleet_manager_scopes_display_name_chk CHECK (display_name <> ''),
    CONSTRAINT fleet_manager_scopes_owner_ref_chk CHECK (jsonb_typeof(owner_ref_json) = 'object'),
    CONSTRAINT fleet_manager_scopes_platform_owner_chk
        CHECK ((scope_type = 'platform' AND scope_owner_id IS NULL AND owner_ref_json = '{}'::jsonb) OR scope_type <> 'platform'),
    CONSTRAINT fleet_manager_scopes_service_owner_chk
        CHECK ((scope_type = 'service' AND scope_owner_id IS NULL) OR scope_type <> 'service'),
    CONSTRAINT fleet_manager_scopes_external_owner_chk
        CHECK ((scope_type IN ('organization', 'project', 'repository') AND scope_owner_id IS NOT NULL) OR scope_type NOT IN ('organization', 'project', 'repository')),
    CONSTRAINT fleet_manager_scopes_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX fleet_manager_scopes_active_default_uidx
    ON fleet_manager_scopes (is_default)
    WHERE is_default = true AND status = 'active';

CREATE INDEX fleet_manager_scopes_type_owner_status_idx
    ON fleet_manager_scopes (scope_type, scope_owner_id, status);

CREATE INDEX fleet_manager_scopes_status_idx
    ON fleet_manager_scopes (status, updated_at);

CREATE INDEX fleet_manager_scopes_owner_ref_gin_idx
    ON fleet_manager_scopes USING gin (owner_ref_json);

CREATE TABLE fleet_manager_servers (
    id uuid PRIMARY KEY,
    server_key text NOT NULL UNIQUE,
    provider_type text NOT NULL,
    status text NOT NULL,
    primary_address_ref text NOT NULL DEFAULT '',
    region text NOT NULL DEFAULT '',
    capacity_class text NOT NULL DEFAULT '',
    secret_store_type text NOT NULL DEFAULT '',
    secret_store_ref text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT fleet_manager_servers_provider_type_chk
        CHECK (provider_type IN ('bare_metal', 'vps', 'cloud', 'managed', 'unknown')),
    CONSTRAINT fleet_manager_servers_status_chk
        CHECK (status IN ('active', 'suspended', 'draining', 'decommissioned')),
    CONSTRAINT fleet_manager_servers_key_chk CHECK (server_key <> ''),
    CONSTRAINT fleet_manager_servers_version_chk CHECK (version > 0)
);

CREATE INDEX fleet_manager_servers_status_provider_idx
    ON fleet_manager_servers (status, provider_type);

CREATE INDEX fleet_manager_servers_region_class_idx
    ON fleet_manager_servers (region, capacity_class, status);

CREATE TABLE fleet_manager_kubernetes_clusters (
    id uuid PRIMARY KEY,
    fleet_scope_id uuid NOT NULL REFERENCES fleet_manager_scopes(id),
    server_id uuid REFERENCES fleet_manager_servers(id),
    cluster_key text NOT NULL UNIQUE,
    status text NOT NULL,
    is_default boolean NOT NULL DEFAULT false,
    api_endpoint_ref text NOT NULL DEFAULT '',
    secret_store_type text NOT NULL DEFAULT '',
    secret_store_ref text NOT NULL DEFAULT '',
    kubernetes_version text NOT NULL DEFAULT '',
    region text NOT NULL DEFAULT '',
    capacity_class text NOT NULL DEFAULT '',
    last_health_status text NOT NULL DEFAULT 'unknown',
    last_health_checked_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT fleet_manager_kubernetes_clusters_status_chk
        CHECK (status IN ('active', 'suspended', 'draining', 'unreachable', 'decommissioned')),
    CONSTRAINT fleet_manager_kubernetes_clusters_health_chk
        CHECK (last_health_status IN ('unknown', 'healthy', 'degraded', 'unhealthy')),
    CONSTRAINT fleet_manager_kubernetes_clusters_key_chk CHECK (cluster_key <> ''),
    CONSTRAINT fleet_manager_kubernetes_clusters_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX fleet_manager_kubernetes_clusters_scope_default_uidx
    ON fleet_manager_kubernetes_clusters (fleet_scope_id)
    WHERE is_default = true AND status = 'active';

CREATE INDEX fleet_manager_kubernetes_clusters_scope_status_idx
    ON fleet_manager_kubernetes_clusters (fleet_scope_id, status);

CREATE INDEX fleet_manager_kubernetes_clusters_health_idx
    ON fleet_manager_kubernetes_clusters (last_health_status, last_health_checked_at);

CREATE INDEX fleet_manager_kubernetes_clusters_server_idx
    ON fleet_manager_kubernetes_clusters (server_id);

CREATE TABLE fleet_manager_cluster_connectivity_checks (
    id uuid PRIMARY KEY,
    cluster_id uuid NOT NULL REFERENCES fleet_manager_kubernetes_clusters(id),
    status text NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    latency_ms bigint,
    error_code text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    CONSTRAINT fleet_manager_cluster_connectivity_checks_status_chk
        CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'timed_out')),
    CONSTRAINT fleet_manager_cluster_connectivity_checks_latency_chk CHECK (latency_ms IS NULL OR latency_ms >= 0),
    CONSTRAINT fleet_manager_cluster_connectivity_checks_finished_chk
        CHECK (finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at)
);

CREATE INDEX fleet_manager_cluster_connectivity_checks_cluster_created_idx
    ON fleet_manager_cluster_connectivity_checks (cluster_id, created_at);

CREATE INDEX fleet_manager_cluster_connectivity_checks_status_idx
    ON fleet_manager_cluster_connectivity_checks (status, created_at);

CREATE TABLE fleet_manager_cluster_health_snapshots (
    id uuid PRIMARY KEY,
    cluster_id uuid NOT NULL REFERENCES fleet_manager_kubernetes_clusters(id),
    health_status text NOT NULL,
    capacity_status text NOT NULL,
    summary_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    checked_at timestamptz NOT NULL,
    error_code text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    CONSTRAINT fleet_manager_cluster_health_snapshots_health_chk
        CHECK (health_status IN ('healthy', 'degraded', 'unhealthy', 'unknown')),
    CONSTRAINT fleet_manager_cluster_health_snapshots_capacity_chk
        CHECK (capacity_status IN ('ok', 'limited', 'exhausted', 'unknown')),
    CONSTRAINT fleet_manager_cluster_health_snapshots_summary_chk CHECK (jsonb_typeof(summary_json) = 'object')
);

CREATE INDEX fleet_manager_cluster_health_snapshots_cluster_checked_idx
    ON fleet_manager_cluster_health_snapshots (cluster_id, checked_at);

CREATE INDEX fleet_manager_cluster_health_snapshots_status_idx
    ON fleet_manager_cluster_health_snapshots (health_status, capacity_status, checked_at);

CREATE TABLE fleet_manager_placement_rules (
    id uuid PRIMARY KEY,
    fleet_scope_id uuid NOT NULL REFERENCES fleet_manager_scopes(id),
    rule_key text NOT NULL,
    status text NOT NULL,
    priority bigint NOT NULL,
    match_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    constraints_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL,
    UNIQUE (fleet_scope_id, rule_key),
    CONSTRAINT fleet_manager_placement_rules_status_chk
        CHECK (status IN ('active', 'disabled', 'archived')),
    CONSTRAINT fleet_manager_placement_rules_key_chk CHECK (rule_key <> ''),
    CONSTRAINT fleet_manager_placement_rules_match_chk CHECK (jsonb_typeof(match_json) = 'object'),
    CONSTRAINT fleet_manager_placement_rules_constraints_chk CHECK (jsonb_typeof(constraints_json) = 'object'),
    CONSTRAINT fleet_manager_placement_rules_version_chk CHECK (version > 0)
);

CREATE INDEX fleet_manager_placement_rules_scope_status_priority_idx
    ON fleet_manager_placement_rules (fleet_scope_id, status, priority);

CREATE TABLE fleet_manager_placement_decisions (
    id uuid PRIMARY KEY,
    command_id uuid,
    request_fingerprint text NOT NULL,
    status text NOT NULL,
    fleet_scope_id uuid REFERENCES fleet_manager_scopes(id),
    cluster_id uuid REFERENCES fleet_manager_kubernetes_clusters(id),
    project_id uuid,
    repository_id uuid,
    runtime_mode text NOT NULL DEFAULT '',
    runtime_profile text NOT NULL DEFAULT '',
    input_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    reason_code text NOT NULL DEFAULT '',
    reason_message text NOT NULL DEFAULT '',
    used_default_path boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    CONSTRAINT fleet_manager_placement_decisions_fingerprint_chk CHECK (request_fingerprint <> ''),
    CONSTRAINT fleet_manager_placement_decisions_status_chk
        CHECK (status IN ('resolved', 'rejected')),
    CONSTRAINT fleet_manager_placement_decisions_input_chk CHECK (jsonb_typeof(input_json) = 'object'),
    CONSTRAINT fleet_manager_placement_decisions_resolved_target_chk
        CHECK ((status = 'resolved' AND fleet_scope_id IS NOT NULL AND cluster_id IS NOT NULL) OR status <> 'resolved')
);

CREATE UNIQUE INDEX fleet_manager_placement_decisions_command_uidx
    ON fleet_manager_placement_decisions (command_id)
    WHERE command_id IS NOT NULL;

CREATE INDEX fleet_manager_placement_decisions_request_idx
    ON fleet_manager_placement_decisions (request_fingerprint);

CREATE INDEX fleet_manager_placement_decisions_project_repo_idx
    ON fleet_manager_placement_decisions (project_id, repository_id, created_at);

CREATE INDEX fleet_manager_placement_decisions_scope_cluster_idx
    ON fleet_manager_placement_decisions (fleet_scope_id, cluster_id, created_at);

CREATE TABLE fleet_manager_command_results (
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
    CONSTRAINT fleet_manager_command_results_identity_chk
        CHECK (command_id IS NOT NULL OR idempotency_key <> ''),
    CONSTRAINT fleet_manager_command_results_actor_chk CHECK (actor_type <> '' AND actor_id <> ''),
    CONSTRAINT fleet_manager_command_results_operation_chk CHECK (operation <> ''),
    CONSTRAINT fleet_manager_command_results_aggregate_type_chk CHECK (aggregate_type <> ''),
    CONSTRAINT fleet_manager_command_results_payload_chk CHECK (jsonb_typeof(result_payload) = 'object')
);

CREATE UNIQUE INDEX fleet_manager_command_results_command_id_uidx
    ON fleet_manager_command_results (command_id)
    WHERE command_id IS NOT NULL;

CREATE UNIQUE INDEX fleet_manager_command_results_idempotency_uidx
    ON fleet_manager_command_results (operation, actor_type, actor_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE INDEX fleet_manager_command_results_aggregate_idx
    ON fleet_manager_command_results (aggregate_type, aggregate_id, created_at);

CREATE TABLE fleet_manager_outbox_events (
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
    CONSTRAINT fleet_manager_outbox_events_event_type_chk CHECK (event_type <> ''),
    CONSTRAINT fleet_manager_outbox_events_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT fleet_manager_outbox_events_aggregate_type_chk CHECK (aggregate_type <> ''),
    CONSTRAINT fleet_manager_outbox_events_payload_chk CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT fleet_manager_outbox_events_attempt_count_chk CHECK (attempt_count >= 0),
    CONSTRAINT fleet_manager_outbox_events_failure_kind_chk
        CHECK (failure_kind IN ('', 'transient', 'permanent'))
);

CREATE INDEX fleet_manager_outbox_events_unpublished_idx
    ON fleet_manager_outbox_events (occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX fleet_manager_outbox_events_claim_idx
    ON fleet_manager_outbox_events (next_attempt_at, occurred_at)
    WHERE published_at IS NULL;

CREATE INDEX fleet_manager_outbox_events_lock_idx
    ON fleet_manager_outbox_events (locked_until)
    WHERE published_at IS NULL AND locked_until IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS fleet_manager_outbox_events;
DROP TABLE IF EXISTS fleet_manager_command_results;
DROP TABLE IF EXISTS fleet_manager_placement_decisions;
DROP TABLE IF EXISTS fleet_manager_placement_rules;
DROP TABLE IF EXISTS fleet_manager_cluster_health_snapshots;
DROP TABLE IF EXISTS fleet_manager_cluster_connectivity_checks;
DROP TABLE IF EXISTS fleet_manager_kubernetes_clusters;
DROP TABLE IF EXISTS fleet_manager_servers;
DROP TABLE IF EXISTS fleet_manager_scopes;
