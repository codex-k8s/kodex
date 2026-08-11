-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Typed owner materializer Issue #247 не выдаёт migration workload прямого
-- DML и не принимает JSON dispatcher. Все записи выполняет обычный runtime
-- principal под signed transaction context и RLS.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE TABLE control_plane.legacy_graph_migration_plans (
    plan_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    owner_actor_id uuid NOT NULL,
    source_root_reference text NOT NULL CHECK (
        length(source_root_reference) BETWEEN 1 AND 512
        AND source_root_reference = btrim(source_root_reference)
    ),
    source_root_sha256 text NOT NULL CHECK (source_root_sha256 ~ '^[a-f0-9]{64}$'),
    source_snapshot_sha256 text NOT NULL CHECK (source_snapshot_sha256 ~ '^[a-f0-9]{64}$'),
    idempotency_key_sha256 text NOT NULL CHECK (idempotency_key_sha256 ~ '^[a-f0-9]{64}$'),
    request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[a-f0-9]{64}$'),
    semantic_sha256 text NOT NULL CHECK (semantic_sha256 ~ '^[a-f0-9]{64}$'),
    project_id uuid NOT NULL,
    state text NOT NULL CHECK (state IN ('PREPARED', 'COMMITTED', 'ABORTED')),
    verification_state text NOT NULL CHECK (verification_state IN ('VERIFIED', 'DRIFTED')),
    operation_count integer NOT NULL CHECK (operation_count BETWEEN 1 AND 2000),
    archived_source_count integer NOT NULL CHECK (archived_source_count BETWEEN 0 AND 50),
    plan_payload bytea NOT NULL CHECK (octet_length(plan_payload) BETWEEN 1 AND 8388608),
    prepared_at timestamptz NOT NULL,
    terminal_at timestamptz,
    UNIQUE (organization_id, source_root_reference, idempotency_key_sha256),
    CHECK ((state = 'PREPARED') = (terminal_at IS NULL))
);

CREATE TABLE control_plane.legacy_graph_source_dispositions (
    plan_id uuid NOT NULL REFERENCES control_plane.legacy_graph_migration_plans(plan_id),
    source_table text NOT NULL CHECK (source_table IN (
        'matter_codex_agent_delegation_callback_deliveries',
        'matter_codex_agent_delegation_callback_manifests',
        'matter_codex_agent_delegations', 'matter_codex_agent_flows',
        'matter_codex_agent_profiles', 'matter_codex_agent_prompt_templates',
        'matter_codex_agent_role_runtime_variables', 'matter_codex_agent_roles',
        'matter_codex_agent_runs', 'matter_codex_agent_session_turns',
        'matter_codex_agent_sessions', 'matter_codex_audit_events',
        'matter_codex_automation_audit_events', 'matter_codex_automation_schedules',
        'matter_codex_chat_participants', 'matter_codex_chat_repositories',
        'matter_codex_chats', 'matter_codex_cluster_admin_bindings',
        'matter_codex_cluster_bot_bindings', 'matter_codex_cluster_delivery_fences',
        'matter_codex_cluster_dependencies', 'matter_codex_cluster_prompt_templates',
        'matter_codex_cluster_revocations', 'matter_codex_cluster_runtime_variable_bindings',
        'matter_codex_cluster_session_bindings', 'matter_codex_cluster_subjects',
        'matter_codex_credentials', 'matter_codex_github_accounts',
        'matter_codex_interaction_capabilities', 'matter_codex_mattermost_bot_identities',
        'matter_codex_memory_embeddings', 'matter_codex_memory_record_versions',
        'matter_codex_memory_records', 'matter_codex_openai_accounts',
        'matter_codex_owner_attention_requests', 'matter_codex_policy_revisions',
        'matter_codex_process_runs', 'matter_codex_process_turns',
        'matter_codex_project_repositories', 'matter_codex_project_runtime_variables',
        'matter_codex_projects', 'matter_codex_repositories',
        'matter_codex_role_capabilities', 'matter_codex_role_relationship_policies',
        'matter_codex_runtime_agent_binding_discoveries',
        'matter_codex_runtime_agent_binding_outbox', 'matter_codex_schedule_occurrences',
        'matter_codex_scheduled_runs', 'matter_codex_thread_contexts',
        'matter_codex_work_claims'
    )),
    disposition text NOT NULL CHECK (disposition IN (
        'MATERIALIZE', 'ARCHIVE_TERMINAL', 'REJECT_NONEMPTY'
    )),
    row_count bigint NOT NULL CHECK (row_count BETWEEN 0 AND 9007199254740991),
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[a-f0-9]{64}$'),
    terminal_state_sha256 text CHECK (
        terminal_state_sha256 IS NULL OR terminal_state_sha256 ~ '^[a-f0-9]{64}$'
    ),
    PRIMARY KEY (plan_id, source_table),
    CHECK ((disposition = 'ARCHIVE_TERMINAL') = (terminal_state_sha256 IS NOT NULL)),
    CHECK (disposition <> 'REJECT_NONEMPTY' OR row_count = 0)
);

CREATE TABLE control_plane.legacy_graph_operation_receipts (
    plan_id uuid NOT NULL REFERENCES control_plane.legacy_graph_migration_plans(plan_id),
    ordinal integer NOT NULL CHECK (ordinal BETWEEN 1 AND 2000),
    operation_kind text NOT NULL CHECK (operation_kind IN (
        'PROJECT', 'TEAM', 'CHAT', 'ARTIFACT', 'CREDENTIAL_BINDING',
        'REPOSITORY_WORKSPACE', 'ROLE_DEFINITION', 'INSTRUCTION_SET',
        'PROVIDER_CONNECTION_REFERENCE', 'PROVIDER_POOL', 'ROLE_IMAGE_RECIPE',
        'IMAGE_BUILD', 'IMAGE_ARTIFACT', 'AGENT', 'AGENT_ASSIGNMENT', 'SCHEDULE',
        'RUNTIME_REVISION', 'SESSION', 'TURN', 'TURN_ATTEMPT', 'PROCESS_RUN',
        'DELEGATION_EDGE', 'CALLBACK_MANIFEST', 'CALLBACK_DELIVERY', 'MEMORY_RECORD'
    )),
    input_sha256 text NOT NULL CHECK (input_sha256 ~ '^[a-f0-9]{64}$'),
    target_id uuid NOT NULL,
    target_kind text NOT NULL CHECK (target_kind = operation_kind),
    target_version bigint NOT NULL DEFAULT 0 CHECK (
        target_version BETWEEN 0 AND 9007199254740991
    ),
    target_state text,
    projection_sha256 text CHECK (
        projection_sha256 IS NULL OR projection_sha256 ~ '^[a-f0-9]{64}$'
    ),
    provenance_sha256 text NOT NULL CHECK (provenance_sha256 ~ '^[a-f0-9]{64}$'),
    provenance_evidence_sha256 text CHECK (
        provenance_evidence_sha256 IS NULL
        OR provenance_evidence_sha256 ~ '^[a-f0-9]{64}$'
    ),
    audit_ids uuid[] NOT NULL DEFAULT ARRAY[]::uuid[],
    event_ids uuid[] NOT NULL DEFAULT ARRAY[]::uuid[],
    event_sequences bigint[] NOT NULL DEFAULT ARRAY[]::bigint[],
    materialized_at timestamptz,
    PRIMARY KEY (plan_id, ordinal),
    UNIQUE (plan_id, target_id),
    CHECK (cardinality(audit_ids) BETWEEN 0 AND 1),
    CHECK (cardinality(event_ids) = cardinality(event_sequences)),
    CHECK (cardinality(event_ids) BETWEEN 0 AND 1),
    CHECK (
        (materialized_at IS NULL AND target_version = 0 AND target_state IS NULL
         AND projection_sha256 IS NULL AND cardinality(audit_ids) = 0
         AND provenance_evidence_sha256 IS NULL AND cardinality(event_ids) = 0)
        OR
        (materialized_at IS NOT NULL AND target_version > 0 AND target_state IS NOT NULL
         AND projection_sha256 IS NOT NULL AND provenance_evidence_sha256 IS NOT NULL
         AND cardinality(audit_ids) = 1)
    )
);

CREATE TABLE control_plane.legacy_graph_provenance (
    plan_id uuid NOT NULL,
    ordinal integer NOT NULL,
    target_id uuid NOT NULL,
    target_kind text NOT NULL,
    source_table text NOT NULL,
    source_ref text NOT NULL CHECK (length(source_ref) BETWEEN 1 AND 512),
    source_revision bigint NOT NULL CHECK (source_revision BETWEEN 1 AND 9007199254740991),
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[a-f0-9]{64}$'),
    immutable_input_sha256 text CHECK (
        immutable_input_sha256 IS NULL OR immutable_input_sha256 ~ '^[a-f0-9]{64}$'
    ),
    root_actor_id uuid NOT NULL,
    root_session_id uuid,
    root_turn_id uuid,
    root_attempt integer CHECK (root_attempt BETWEEN 1 AND 100),
    runtime_revision_id uuid,
    runtime_revision_version bigint CHECK (runtime_revision_version BETWEEN 1 AND 9007199254740991),
    parent_target_id uuid,
    launching_turn_id uuid,
    launching_attempt integer CHECK (launching_attempt BETWEEN 1 AND 100),
    launching_attempt_target_id uuid,
    machine_policy_revision bigint CHECK (machine_policy_revision BETWEEN 1 AND 9007199254740991),
    machine_policy_sha256 text CHECK (machine_policy_sha256 IS NULL OR machine_policy_sha256 ~ '^[a-f0-9]{64}$'),
    legacy_policy_revision bigint CHECK (legacy_policy_revision BETWEEN 1 AND 9007199254740991),
    legacy_policy_sha256 text CHECK (legacy_policy_sha256 IS NULL OR legacy_policy_sha256 ~ '^[a-f0-9]{64}$'),
    lineage_sha256 text NOT NULL CHECK (lineage_sha256 ~ '^[a-f0-9]{64}$'),
    PRIMARY KEY (plan_id, ordinal),
    FOREIGN KEY (plan_id, ordinal)
        REFERENCES control_plane.legacy_graph_operation_receipts(plan_id, ordinal),
    CHECK ((runtime_revision_id IS NULL) = (runtime_revision_version IS NULL)),
    CHECK ((machine_policy_revision IS NULL) = (machine_policy_sha256 IS NULL)),
    CHECK ((legacy_policy_revision IS NULL) = (legacy_policy_sha256 IS NULL)),
    CHECK ((launching_attempt IS NULL) = (launching_turn_id IS NULL))
);

CREATE TABLE control_plane.delegation_callback_manifests (
    id uuid PRIMARY KEY,
    plan_id uuid NOT NULL REFERENCES control_plane.legacy_graph_migration_plans(plan_id),
    delegation_id uuid NOT NULL REFERENCES control_plane.delegation_edges(id),
    callback_process_id uuid NOT NULL REFERENCES control_plane.resources(id),
    destinations text[] NOT NULL CHECK (cardinality(destinations) BETWEEN 1 AND 8),
    manifest_sha256 text NOT NULL CHECK (manifest_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL,
    UNIQUE (plan_id, delegation_id)
);

CREATE TABLE control_plane.delegation_callback_deliveries (
    id uuid PRIMARY KEY,
    plan_id uuid NOT NULL REFERENCES control_plane.legacy_graph_migration_plans(plan_id),
    manifest_id uuid NOT NULL REFERENCES control_plane.delegation_callback_manifests(id),
    destination text NOT NULL CHECK (length(destination) BETWEEN 1 AND 128),
    receipt_sha256 text NOT NULL CHECK (receipt_sha256 ~ '^[a-f0-9]{64}$'),
    state text NOT NULL CHECK (state IN ('DELIVERED', 'FAILED', 'CANCELLED')),
    delivered_at timestamptz NOT NULL,
    UNIQUE (manifest_id, destination)
);

-- PREPARED intent и receipt identity неизменяемы даже для runtime role.
-- Разрешены только однонаправленный terminal winner и одноразовая запись
-- фактического operation evidence.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_legacy_graph_plan_update()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    IF ROW(
        NEW.plan_id, NEW.organization_id, NEW.owner_actor_id,
        NEW.source_root_reference, NEW.source_root_sha256,
        NEW.source_snapshot_sha256, NEW.idempotency_key_sha256,
        NEW.request_sha256, NEW.semantic_sha256, NEW.project_id,
        NEW.operation_count, NEW.archived_source_count, NEW.plan_payload,
        NEW.prepared_at
    ) IS DISTINCT FROM ROW(
        OLD.plan_id, OLD.organization_id, OLD.owner_actor_id,
        OLD.source_root_reference, OLD.source_root_sha256,
        OLD.source_snapshot_sha256, OLD.idempotency_key_sha256,
        OLD.request_sha256, OLD.semantic_sha256, OLD.project_id,
        OLD.operation_count, OLD.archived_source_count, OLD.plan_payload,
        OLD.prepared_at
    ) OR OLD.state <> 'PREPARED'
      OR NEW.state NOT IN ('COMMITTED', 'ABORTED')
      OR NEW.verification_state <> 'VERIFIED'
      OR NEW.terminal_at IS NULL THEN
        RAISE EXCEPTION 'legacy graph plan is immutable after PREPARED'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

CREATE TRIGGER legacy_graph_plan_update_fence
BEFORE UPDATE ON control_plane.legacy_graph_migration_plans
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_legacy_graph_plan_update();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_legacy_graph_operation_update()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    IF ROW(
        NEW.plan_id, NEW.ordinal, NEW.operation_kind, NEW.input_sha256,
        NEW.target_id, NEW.target_kind, NEW.provenance_sha256
    ) IS DISTINCT FROM ROW(
        OLD.plan_id, OLD.ordinal, OLD.operation_kind, OLD.input_sha256,
        OLD.target_id, OLD.target_kind, OLD.provenance_sha256
    ) OR OLD.materialized_at IS NOT NULL
      OR NEW.materialized_at IS NULL
      OR NEW.target_version = 0
      OR NEW.target_state IS NULL
      OR NEW.projection_sha256 IS NULL
      OR NEW.provenance_evidence_sha256 IS NULL
      OR cardinality(NEW.audit_ids) <> 1
      OR cardinality(NEW.event_ids) <> cardinality(NEW.event_sequences) THEN
        RAISE EXCEPTION 'legacy graph operation receipt is immutable after materialization'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

CREATE TRIGGER legacy_graph_operation_update_fence
BEFORE UPDATE ON control_plane.legacy_graph_operation_receipts
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_legacy_graph_operation_update();

REVOKE ALL ON FUNCTION control_plane.guard_legacy_graph_plan_update(),
    control_plane.guard_legacy_graph_operation_update() FROM PUBLIC;

CREATE INDEX legacy_graph_plans_owner_idx ON control_plane.legacy_graph_migration_plans (
    organization_id, owner_actor_id, prepared_at DESC, plan_id
);
CREATE INDEX legacy_graph_provenance_target_idx ON control_plane.legacy_graph_provenance (
    target_id, target_kind
);

ALTER TABLE control_plane.legacy_graph_migration_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.legacy_graph_migration_plans FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.legacy_graph_source_dispositions ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.legacy_graph_source_dispositions FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.legacy_graph_operation_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.legacy_graph_operation_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.legacy_graph_provenance ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.legacy_graph_provenance FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.delegation_callback_manifests ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.delegation_callback_manifests FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.delegation_callback_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.delegation_callback_deliveries FORCE ROW LEVEL SECURITY;

CREATE POLICY legacy_graph_plans_runtime_scope ON control_plane.legacy_graph_migration_plans
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    );
CREATE POLICY legacy_graph_dispositions_runtime_scope ON control_plane.legacy_graph_source_dispositions
    FOR ALL TO control_plane_runtime
    USING (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = legacy_graph_source_dispositions.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = legacy_graph_source_dispositions.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ));
CREATE POLICY legacy_graph_receipts_runtime_scope ON control_plane.legacy_graph_operation_receipts
    FOR ALL TO control_plane_runtime
    USING (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = legacy_graph_operation_receipts.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = legacy_graph_operation_receipts.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ));
CREATE POLICY legacy_graph_provenance_runtime_scope ON control_plane.legacy_graph_provenance
    FOR ALL TO control_plane_runtime
    USING (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = legacy_graph_provenance.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = legacy_graph_provenance.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ));
CREATE POLICY callback_manifests_runtime_scope ON control_plane.delegation_callback_manifests
    FOR ALL TO control_plane_runtime
    USING (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = delegation_callback_manifests.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = delegation_callback_manifests.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ));
CREATE POLICY callback_deliveries_runtime_scope ON control_plane.delegation_callback_deliveries
    FOR ALL TO control_plane_runtime
    USING (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = delegation_callback_deliveries.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM control_plane.legacy_graph_migration_plans AS plan
        WHERE plan.plan_id = delegation_callback_deliveries.plan_id
          AND plan.organization_id = (control_plane.runtime_scope()).organization_id
          AND plan.owner_actor_id = (control_plane.runtime_scope()).actor_id
    ));

REVOKE ALL ON control_plane.legacy_graph_migration_plans,
    control_plane.legacy_graph_source_dispositions,
    control_plane.legacy_graph_operation_receipts,
    control_plane.legacy_graph_provenance,
    control_plane.delegation_callback_manifests,
    control_plane.delegation_callback_deliveries FROM PUBLIC;
GRANT SELECT, INSERT ON control_plane.legacy_graph_migration_plans,
    control_plane.legacy_graph_operation_receipts TO control_plane_runtime;
GRANT UPDATE (state, verification_state, terminal_at)
    ON control_plane.legacy_graph_migration_plans TO control_plane_runtime;
GRANT UPDATE (
    target_version, target_state, projection_sha256,
    provenance_evidence_sha256, audit_ids,
    event_ids, event_sequences, materialized_at
) ON control_plane.legacy_graph_operation_receipts TO control_plane_runtime;
GRANT SELECT, INSERT ON control_plane.legacy_graph_source_dispositions,
    control_plane.legacy_graph_provenance,
    control_plane.delegation_callback_manifests,
    control_plane.delegation_callback_deliveries TO control_plane_runtime;

UPDATE control_plane.schema_state
SET version = 20260807024700, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'migration 20260807024700 is forward-only: immutable legacy graph receipts cannot be discarded';
END $$;
-- +goose StatementEnd
