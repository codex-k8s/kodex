-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Целевая owner-конфигурация Issue #234 вводится одним forward-only cutover:
-- новые защищённые виды получают специализированный history, а legacy
-- ROLE/PROMPT_PROFILE становятся неизменяемыми version-pinned входами уже
-- существующих графов исполнения.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

ALTER TABLE control_plane.resources
    DROP CONSTRAINT resources_kind_check,
    DROP CONSTRAINT resources_spec_check,
    ADD CONSTRAINT resources_kind_v2_ck CHECK (kind IN (
        'PROJECT', 'TEAM', 'CHAT', 'ROLE', 'PROMPT_PROFILE',
        'CREDENTIAL_BINDING', 'REPOSITORY_WORKSPACE', 'INTEGRATION',
        'RUNTIME_REVISION', 'SESSION', 'TURN', 'PROCESS_RUN', 'SCHEDULE',
        'OWNER_GATE', 'MEMORY_RECORD', 'WORK_CLAIM', 'ARTIFACT',
        'ROLE_IMAGE_RECIPE', 'IMAGE_BUILD', 'IMAGE_ARTIFACT',
        'ROLE_DEFINITION', 'AGENT', 'AGENT_ASSIGNMENT', 'INSTRUCTION_SET',
        'PROVIDER_CONNECTION_REFERENCE', 'PROVIDER_POOL',
        'WORKSPACE_BACKUP', 'WORKSPACE_RESTORE',
        'WORKSPACE_MATTERMOST_MAPPING'
    )),
    ADD CONSTRAINT resources_spec_v2_ck CHECK (
        jsonb_typeof(spec) = 'object'
        AND octet_length(spec::text) <= CASE
            WHEN kind IN ('INSTRUCTION_SET', 'WORKSPACE_BACKUP', 'WORKSPACE_RESTORE')
                THEN 524288
            ELSE 65536
        END
    );

DROP INDEX control_plane.resources_stable_key_uidx;
CREATE UNIQUE INDEX resources_stable_key_v2_uidx
    ON control_plane.resources (
        organization_id, project_id, kind, (spec ->> 'stableKey')
    )
    WHERE kind IN (
        'TEAM', 'CHAT', 'ROLE', 'ROLE_DEFINITION', 'AGENT',
        'INSTRUCTION_SET', 'PROVIDER_CONNECTION_REFERENCE', 'PROVIDER_POOL'
    ) AND state <> 'DELETED';

CREATE UNIQUE INDEX resources_agent_assignment_active_uidx
    ON control_plane.resources (
        organization_id,
        project_id,
        ((spec ->> 'agentId')::uuid),
        ((spec ->> 'workspaceId')::uuid),
        coalesce(spec ->> 'roomId', '')
    )
    WHERE kind = 'AGENT_ASSIGNMENT'
      AND state NOT IN ('ARCHIVED', 'DELETION_PENDING', 'DELETED');

CREATE UNIQUE INDEX resources_workspace_mapping_uidx
    ON control_plane.resources (
        organization_id, project_id, ((spec ->> 'workspaceId')::uuid)
    )
    WHERE kind = 'WORKSPACE_MATTERMOST_MAPPING'
      AND state NOT IN ('ARCHIVED', 'DELETION_PENDING', 'DELETED');

-- Историческая Session остаётся immutable, но не занимает admission boundary.
-- Сначала закрыто доказываем, что уже существующий live graph однозначен;
-- только затем заменяем более широкий индекс из 20260731000100.
-- +goose StatementBegin
DO $session_conversation_preflight$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM control_plane.resources
        WHERE kind = 'SESSION'
          AND state IN (
              'ACTIVE', 'PAUSED', 'QUEUED', 'CLAIMED', 'RUNNING',
              'WAITING_EXTERNAL', 'WAITING_OWNER', 'BLOCKED'
          )
          AND coalesce(spec ->> 'conversationId', '') <> ''
        GROUP BY organization_id, project_id, spec ->> 'conversationId'
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION
            'control-plane Session conversation admission boundary is ambiguous'
            USING ERRCODE = '23505';
    END IF;
END
$session_conversation_preflight$;
-- +goose StatementEnd
DROP INDEX control_plane.resources_session_conversation_uidx;
CREATE UNIQUE INDEX resources_session_conversation_uidx
    ON control_plane.resources (
        organization_id,
        project_id,
        (spec ->> 'conversationId')
    )
    WHERE kind = 'SESSION'
      AND state IN (
          'ACTIVE', 'PAUSED', 'QUEUED', 'CLAIMED', 'RUNNING',
          'WAITING_EXTERNAL', 'WAITING_OWNER', 'BLOCKED'
      )
      AND coalesce(spec ->> 'conversationId', '') <> '';

CREATE TABLE control_plane.protected_resource_history (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    resource_id uuid NOT NULL,
    resource_version bigint NOT NULL
        CHECK (resource_version BETWEEN 1 AND 9007199254740991),
    resource_kind text NOT NULL CHECK (resource_kind IN (
        'ROLE_DEFINITION', 'AGENT', 'AGENT_ASSIGNMENT', 'INSTRUCTION_SET',
        'PROVIDER_CONNECTION_REFERENCE', 'PROVIDER_POOL',
        'WORKSPACE_BACKUP', 'WORKSPACE_RESTORE',
        'WORKSPACE_MATTERMOST_MAPPING'
    )),
    owner_actor_id uuid NOT NULL,
    action text NOT NULL CHECK (action ~ '^[a-z][a-z0-9_]{0,63}$'),
    snapshot jsonb NOT NULL CHECK (
        jsonb_typeof(snapshot) = 'object'
        AND octet_length(snapshot::text) <= 786432
    ),
    snapshot_sha256 text NOT NULL CHECK (snapshot_sha256 ~ '^[a-f0-9]{64}$'),
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, resource_id, resource_version),
    FOREIGN KEY (organization_id, project_id, resource_id)
        REFERENCES control_plane.resources (organization_id, project_id, id)
);
CREATE INDEX protected_resource_history_owner_page_idx
    ON control_plane.protected_resource_history (
        organization_id, project_id, owner_actor_id, resource_id,
        resource_version DESC
    );
ALTER TABLE control_plane.protected_resource_history OWNER TO control_plane_owner;
ALTER TABLE control_plane.protected_resource_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.protected_resource_history FORCE ROW LEVEL SECURITY;
CREATE POLICY protected_resource_history_runtime_scope
    ON control_plane.protected_resource_history
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    );
REVOKE ALL ON control_plane.protected_resource_history FROM PUBLIC;
GRANT SELECT, INSERT ON control_plane.protected_resource_history TO control_plane_runtime;

-- Read-only compatibility projection для уже развернутого runtime-controller.
-- Таблица не входит в generic resources mutation path: source authority остаётся
-- только у Agent/InstructionSet/ProviderConnectionReference.
CREATE TABLE control_plane.runtime_derived_resources (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    parent_id uuid,
    owner_actor_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('ROLE', 'PROMPT_PROFILE')),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 160),
    state text NOT NULL DEFAULT 'ACTIVE' CHECK (state = 'ACTIVE'),
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
    spec jsonb NOT NULL CHECK (jsonb_typeof(spec) = 'object' AND octet_length(spec::text) <= 65536),
    source_kind text NOT NULL CHECK (source_kind IN ('AGENT', 'INSTRUCTION_SET')),
    source_id uuid NOT NULL,
    source_version bigint NOT NULL CHECK (source_version BETWEEN 1 AND 9007199254740991),
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, id, version),
    UNIQUE (organization_id, project_id, source_kind, source_id, source_version)
);
CREATE INDEX runtime_derived_resources_read_idx
    ON control_plane.runtime_derived_resources (organization_id, project_id, id, version);
ALTER TABLE control_plane.runtime_derived_resources OWNER TO control_plane_owner;
ALTER TABLE control_plane.runtime_derived_resources ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.runtime_derived_resources FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_derived_resources_runtime_scope
    ON control_plane.runtime_derived_resources
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    );
REVOKE ALL ON control_plane.runtime_derived_resources FROM PUBLIC;
GRANT SELECT, INSERT ON control_plane.runtime_derived_resources TO control_plane_runtime;

-- Per-row deterministic cutover map. Legacy content bytes никогда не
-- изобретаются из digest: upgrade фиксирует точные target IDs и typed manual
-- action, после чего owner reconciliation атомарно материализует весь catalog.
CREATE TABLE control_plane.legacy_configuration_cutovers (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    owner_actor_id uuid NOT NULL,
    legacy_role_id uuid NOT NULL,
    legacy_role_version bigint NOT NULL CHECK (legacy_role_version BETWEEN 1 AND 9007199254740991),
    legacy_prompt_profile_id uuid NOT NULL,
    legacy_prompt_version bigint NOT NULL CHECK (legacy_prompt_version BETWEEN 1 AND 9007199254740991),
    source_role_sha256 text NOT NULL CHECK (source_role_sha256 ~ '^[a-f0-9]{64}$'),
    source_prompt_sha256 text NOT NULL CHECK (source_prompt_sha256 ~ '^[a-f0-9]{64}$'),
    source_credential_ids uuid[] NOT NULL,
    target_role_definition_id uuid NOT NULL,
    target_agent_id uuid NOT NULL,
    target_instruction_set_id uuid NOT NULL,
    target_provider_pool_id uuid NOT NULL,
    target_agent_assignment_id uuid NOT NULL,
    target_provider_reference_ids uuid[] NOT NULL,
    state text NOT NULL CHECK (state IN ('BLOCKED', 'MIGRATED')),
    block_code text CHECK (block_code IS NULL OR block_code ~ '^[a-z][a-z0-9._-]{0,127}$'),
    manual_action text CHECK (manual_action IS NULL OR length(manual_action) BETWEEN 1 AND 512),
    result_agent_version bigint NOT NULL DEFAULT 0 CHECK (result_agent_version BETWEEN 0 AND 9007199254740991),
    result_agent_sha256 text CHECK (result_agent_sha256 IS NULL OR result_agent_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL,
    resolved_at timestamptz,
    PRIMARY KEY (organization_id, project_id, legacy_role_id),
    UNIQUE (organization_id, project_id, target_role_definition_id),
    UNIQUE (organization_id, project_id, target_agent_id),
    UNIQUE (organization_id, project_id, target_provider_pool_id),
    UNIQUE (organization_id, project_id, target_agent_assignment_id),
    CHECK ((state = 'BLOCKED') = (block_code IS NOT NULL)),
    CHECK ((state = 'BLOCKED') = (manual_action IS NOT NULL)),
    CHECK ((state = 'MIGRATED') = (resolved_at IS NOT NULL)),
    CHECK ((state = 'MIGRATED') = (result_agent_version > 0)),
    CHECK ((state = 'MIGRATED') = (result_agent_sha256 IS NOT NULL))
);
WITH legacy_roles AS (
    SELECT role.*,
        role.spec ->> 'promptProfileId' AS prompt_id_text,
        role.spec ->> 'roleImageRecipeId' AS recipe_id_text,
        role.spec -> 'providerCredentialBindingIds' AS credential_ids_json
    FROM control_plane.resources AS role
    WHERE role.kind = 'ROLE' AND role.state <> 'DELETED'
), normalized AS (
    SELECT role.*,
        CASE WHEN role.prompt_id_text ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            THEN role.prompt_id_text::uuid
            ELSE md5('mattercodex:invalid-legacy-prompt:' || role.id::text)::uuid
        END AS prompt_id,
        CASE WHEN role.recipe_id_text ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
            THEN role.recipe_id_text::uuid
            ELSE md5('mattercodex:invalid-legacy-runtime-profile:' || role.id::text)::uuid
        END AS recipe_id,
        coalesce(credentials.ids, ARRAY[]::uuid[]) AS credential_ids,
        jsonb_typeof(role.credential_ids_json) = 'array'
            AND coalesce(credentials.all_valid, false) AS credentials_valid,
        coalesce(credentials.item_count, 0) AS credential_count
    FROM legacy_roles AS role
    LEFT JOIN LATERAL (
        SELECT array_agg(value::uuid ORDER BY value)
                   FILTER (WHERE value ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') AS ids,
               bool_and(value ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') AS all_valid,
               count(*) AS item_count
        FROM jsonb_array_elements_text(
            CASE WHEN jsonb_typeof(role.credential_ids_json) = 'array'
                 THEN role.credential_ids_json ELSE '[]'::jsonb END
        ) AS source(value)
    ) AS credentials ON true
), classified AS (
    SELECT role.*, prompt.id AS found_prompt_id, prompt.version AS prompt_version,
           prompt.spec AS prompt_spec, recipe.id AS found_recipe_id,
        CASE
            WHEN role.prompt_id_text !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                THEN 'legacy_prompt_profile_reference_invalid'
            WHEN prompt.id IS NULL THEN 'legacy_prompt_profile_missing'
            WHEN coalesce(prompt.spec ->> 'contentSha256', '') !~ '^[a-f0-9]{64}$'
                THEN 'legacy_instruction_digest_invalid'
            WHEN coalesce(prompt.spec ->> 'contentArtifactId', '') !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                THEN 'legacy_instruction_artifact_missing'
            WHEN role.credential_count = 0 THEN 'legacy_provider_binding_missing'
            WHEN NOT role.credentials_valid THEN 'legacy_provider_binding_reference_invalid'
            WHEN EXISTS (
                SELECT 1
                FROM unnest(role.credential_ids) AS source(credential_id)
                LEFT JOIN control_plane.resources AS credential
                  ON credential.organization_id = role.organization_id
                 AND credential.project_id = role.project_id
                 AND credential.id = source.credential_id
                 AND credential.kind = 'CREDENTIAL_BINDING'
                 AND credential.state = 'ACTIVE'
                WHERE credential.id IS NULL
                   OR coalesce(credential.spec ->> 'purpose', '') <> 'provider-account'
                   OR coalesce(credential.spec ->> 'providerEligible', '') <> 'true'
            ) THEN 'legacy_provider_binding_ineligible'
            WHEN role.recipe_id_text !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                OR recipe.id IS NULL THEN 'legacy_runtime_profile_missing'
            ELSE 'legacy_instruction_content_readback_required'
        END AS block_code
    FROM normalized AS role
    LEFT JOIN control_plane.resources AS prompt
      ON prompt.organization_id = role.organization_id
     AND prompt.project_id = role.project_id
     AND prompt.id = role.prompt_id
     AND prompt.kind = 'PROMPT_PROFILE'
     AND prompt.state <> 'DELETED'
    LEFT JOIN control_plane.resources AS recipe
      ON recipe.organization_id = role.organization_id
     AND recipe.project_id = role.project_id
     AND recipe.id = role.recipe_id
     AND recipe.kind = 'ROLE_IMAGE_RECIPE'
     AND recipe.state = 'ACTIVE'
)
INSERT INTO control_plane.legacy_configuration_cutovers (
    organization_id, project_id, owner_actor_id, legacy_role_id, legacy_role_version,
    legacy_prompt_profile_id, legacy_prompt_version, source_role_sha256, source_prompt_sha256,
    source_credential_ids, target_role_definition_id, target_agent_id, target_instruction_set_id,
    target_provider_pool_id, target_agent_assignment_id, target_provider_reference_ids,
    state, block_code, manual_action, created_at
)
SELECT role.organization_id, role.project_id, role.owner_actor_id, role.id, role.version,
    role.prompt_id, coalesce(role.prompt_version, 1),
    encode(control_plane_extensions.digest(convert_to(role.spec::text, 'UTF8'), 'sha256'), 'hex'),
    CASE WHEN coalesce(role.prompt_spec ->> 'contentSha256', '') ~ '^[a-f0-9]{64}$'
         THEN role.prompt_spec ->> 'contentSha256' ELSE repeat('0', 64) END,
    role.credential_ids,
    md5('mattercodex:legacy-role-definition:' || role.id::text)::uuid,
    md5('mattercodex:legacy-agent:' || role.id::text)::uuid,
    md5('mattercodex:legacy-instruction-set:' || role.prompt_id::text)::uuid,
    md5('mattercodex:legacy-provider-pool:' || role.id::text)::uuid,
    md5('mattercodex:legacy-agent-assignment:' || role.id::text)::uuid,
    ARRAY(SELECT md5('mattercodex:legacy-provider-reference:' || value::text)::uuid
          FROM unnest(role.credential_ids) AS source(value) ORDER BY value),
    'BLOCKED',
    role.block_code,
    CASE role.block_code
        WHEN 'legacy_instruction_content_readback_required'
            THEN 'Call ResolveLegacyConfigurationCutover with exact immutable instruction content matching source_prompt_sha256'
        WHEN 'legacy_provider_binding_ineligible'
            THEN 'Restore exact referenced provider binding eligibility, then call ResolveLegacyConfigurationCutover'
        ELSE 'Create an owner-approved target replacement; immutable legacy source cannot be repaired automatically'
    END,
    transaction_timestamp()
FROM classified AS role;
CREATE INDEX legacy_configuration_cutovers_owner_page_idx
    ON control_plane.legacy_configuration_cutovers (
        organization_id, project_id, owner_actor_id, legacy_role_id
    );
ALTER TABLE control_plane.legacy_configuration_cutovers OWNER TO control_plane_owner;
ALTER TABLE control_plane.legacy_configuration_cutovers ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.legacy_configuration_cutovers FORCE ROW LEVEL SECURITY;
CREATE POLICY legacy_configuration_cutovers_runtime_scope
    ON control_plane.legacy_configuration_cutovers
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    );
REVOKE ALL ON control_plane.legacy_configuration_cutovers FROM PUBLIC;
GRANT SELECT, UPDATE ON control_plane.legacy_configuration_cutovers TO control_plane_runtime;

-- JTI внешнего signed readback резервируется ровно один раз до mutation и
-- получает exact result tuple в той же owner transaction. Повтор с другим
-- target/intent не может пройти через уникальный issuer+purpose+receipt_id.
CREATE TABLE control_plane.external_command_receipt_consumptions (
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 512),
    purpose text NOT NULL CHECK (purpose ~ '^[A-Z][A-Z0-9_]{0,95}$'),
    receipt_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    owner_actor_id uuid NOT NULL,
    target_kind text NOT NULL CHECK (target_kind ~ '^[a-z][a-z0-9_]{0,95}$'),
    target_resource_id uuid,
    target_stable_key text NOT NULL CHECK (target_stable_key ~ '^[a-z0-9]([a-z0-9._-]{0,94}[a-z0-9])?$'),
    action text NOT NULL CHECK (action ~ '^[a-z][a-z0-9_]{0,63}$'),
    effect text NOT NULL CHECK (effect ~ '^[a-z][a-z0-9_]{0,95}$'),
    effect_generation bigint NOT NULL CHECK (effect_generation BETWEEN 1 AND 9007199254740991),
    effect_sha256 text NOT NULL CHECK (effect_sha256 ~ '^[a-f0-9]{64}$'),
    command_intent_sha256 text NOT NULL CHECK (command_intent_sha256 ~ '^[a-f0-9]{64}$'),
    authority_sha256 text NOT NULL CHECK (authority_sha256 ~ '^[a-f0-9]{64}$'),
    result_resource_id uuid,
    result_version bigint NOT NULL DEFAULT 0 CHECK (result_version BETWEEN 0 AND 9007199254740991),
    result_sha256 text CHECK (result_sha256 IS NULL OR result_sha256 ~ '^[a-f0-9]{64}$'),
    result_snapshot jsonb CHECK (
        result_snapshot IS NULL OR (
            jsonb_typeof(result_snapshot) = 'object'
            AND octet_length(result_snapshot::text) <= 1048576
        )
    ),
    consumed_at timestamptz NOT NULL,
    PRIMARY KEY (issuer, purpose, receipt_id),
    CHECK ((result_resource_id IS NULL) = (result_version = 0)),
    CHECK ((result_resource_id IS NULL) = (result_sha256 IS NULL)),
    CHECK ((result_resource_id IS NULL) = (result_snapshot IS NULL))
);
ALTER TABLE control_plane.external_command_receipt_consumptions OWNER TO control_plane_owner;
ALTER TABLE control_plane.external_command_receipt_consumptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.external_command_receipt_consumptions FORCE ROW LEVEL SECURITY;
CREATE POLICY external_command_receipt_runtime_scope
    ON control_plane.external_command_receipt_consumptions
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    );
REVOKE ALL ON control_plane.external_command_receipt_consumptions FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON control_plane.external_command_receipt_consumptions TO control_plane_runtime;

-- Weighted cursor связывается с существующим authoritative selection key.
-- Legacy path использует ROLE, target path — AGENT; derived ROLE ещё не
-- существует в момент выбора и никогда не получает mutation authority.
DROP FUNCTION control_plane.next_provider_pool_slot(uuid, bigint, text, bigint);
ALTER TABLE control_plane.provider_pool_cursors
    RENAME COLUMN role_id TO selection_key_id;

-- +goose StatementBegin
CREATE FUNCTION control_plane.next_provider_pool_slot(
    requested_selection_key_id uuid,
    requested_policy_revision bigint,
    requested_snapshot_sha256 text,
    requested_total_weight bigint
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
SET row_security = on
AS $function$
DECLARE
    scope record;
    cursor_row control_plane.provider_pool_cursors%ROWTYPE;
    selected_slot bigint;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_runtime', 'member')
       OR requested_policy_revision < 1
       OR requested_snapshot_sha256 !~ '^[a-f0-9]{64}$'
       OR requested_total_weight NOT BETWEEN 1 AND 80000 THEN
        RAISE EXCEPTION 'provider pool cursor input is invalid'
            USING ERRCODE = '22023';
    END IF;
    SELECT * INTO scope FROM control_plane.runtime_scope();
    IF NOT EXISTS (
        SELECT 1
        FROM control_plane.resources AS selection_key
        WHERE selection_key.id = requested_selection_key_id
          AND selection_key.organization_id = scope.organization_id
          AND selection_key.project_id = scope.project_id
          AND selection_key.kind IN ('ROLE', 'AGENT')
          AND selection_key.state = 'ACTIVE'
    ) THEN
        RAISE EXCEPTION 'provider pool selection key is unavailable'
            USING ERRCODE = 'P0002';
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            scope.organization_id::text || ':' || scope.project_id::text || ':' ||
            requested_selection_key_id::text,
            0
        )
    );
    SELECT * INTO cursor_row
    FROM control_plane.provider_pool_cursors
    WHERE organization_id = scope.organization_id
      AND project_id = scope.project_id
      AND selection_key_id = requested_selection_key_id
    FOR UPDATE;
    IF NOT FOUND THEN
        selected_slot := 0;
        INSERT INTO control_plane.provider_pool_cursors (
            organization_id, project_id, selection_key_id, policy_revision,
            snapshot_sha256, total_weight, next_slot, updated_at
        ) VALUES (
            scope.organization_id, scope.project_id, requested_selection_key_id,
            requested_policy_revision, requested_snapshot_sha256,
            requested_total_weight, 1 % requested_total_weight,
            clock_timestamp()
        );
    ELSIF cursor_row.policy_revision <> requested_policy_revision
       OR cursor_row.snapshot_sha256 <> requested_snapshot_sha256
       OR cursor_row.total_weight <> requested_total_weight THEN
        selected_slot := 0;
        UPDATE control_plane.provider_pool_cursors
        SET policy_revision = requested_policy_revision,
            snapshot_sha256 = requested_snapshot_sha256,
            total_weight = requested_total_weight,
            next_slot = 1 % requested_total_weight,
            updated_at = clock_timestamp()
        WHERE organization_id = scope.organization_id
          AND project_id = scope.project_id
          AND selection_key_id = requested_selection_key_id;
    ELSE
        selected_slot := cursor_row.next_slot;
        UPDATE control_plane.provider_pool_cursors
        SET next_slot = (cursor_row.next_slot + 1) % requested_total_weight,
            updated_at = clock_timestamp()
        WHERE organization_id = scope.organization_id
          AND project_id = scope.project_id
          AND selection_key_id = requested_selection_key_id;
    END IF;
    RETURN selected_slot;
END
$function$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.next_provider_pool_slot(uuid, bigint, text, bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.next_provider_pool_slot(uuid, bigint, text, bigint)
    TO control_plane_runtime;

-- Runtime owner-gate, integration continuation и blocked completion завершают
-- текущую attempt до свежего continuation/retry. Старый inline CHECK не
-- включал реально сохраняемые WAITING_* и BLOCKED состояния.
-- +goose StatementBegin
DO $turn_attempt_state_validation$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM control_plane.turn_attempts
        WHERE state NOT IN (
            'QUEUED', 'CLAIMED', 'WAITING_OWNER', 'WAITING_EXTERNAL',
            'BLOCKED', 'EXPIRED', 'SUCCEEDED', 'FAILED', 'CANCELLED'
        )
    ) THEN
        RAISE EXCEPTION 'turn attempt state backfill contains an unsupported value'
            USING ERRCODE = '23514';
    END IF;
END
$turn_attempt_state_validation$;
-- +goose StatementEnd

ALTER TABLE control_plane.turn_attempts
    DROP CONSTRAINT turn_attempts_state_check,
    ADD CONSTRAINT turn_attempts_state_v2_ck CHECK (state IN (
        'QUEUED', 'CLAIMED', 'WAITING_OWNER', 'WAITING_EXTERNAL',
        'BLOCKED', 'EXPIRED', 'SUCCEEDED', 'FAILED', 'CANCELLED'
    ));

-- Turn меняет current RuntimeRevision при retry, поэтому immutable attempt
-- хранит собственный exact revision pin. Backfill сначала использует
-- RuntimeExecution, затем current Turn, затем единственную хронологическую
-- revision того же Session между соседними attempts. Неполная история
-- закрыто останавливает migration вместо публикации ложной lineage.
ALTER TABLE control_plane.turn_attempts
    ADD COLUMN runtime_revision_id uuid,
    ADD COLUMN runtime_revision_version bigint
        CHECK (runtime_revision_version BETWEEN 1 AND 9007199254740991),
    ADD CHECK ((runtime_revision_id IS NULL) = (runtime_revision_version IS NULL));

UPDATE control_plane.turn_attempts AS attempt
SET runtime_revision_id = execution.runtime_revision_id,
    runtime_revision_version = execution.runtime_revision_version
FROM control_plane.runtime_executions AS execution
WHERE execution.turn_id = attempt.turn_id
  AND execution.attempt = attempt.attempt;

UPDATE control_plane.turn_attempts AS attempt
SET runtime_revision_id = (turn.spec ->> 'runtimeRevisionId')::uuid,
    runtime_revision_version = revision.version
FROM control_plane.resources AS turn
JOIN control_plane.resources AS revision
  ON revision.organization_id = turn.organization_id
 AND revision.project_id = turn.project_id
 AND revision.owner_actor_id = turn.owner_actor_id
 AND revision.id = (turn.spec ->> 'runtimeRevisionId')::uuid
 AND revision.kind = 'RUNTIME_REVISION'
WHERE attempt.runtime_revision_id IS NULL
  AND turn.id = attempt.turn_id
  AND turn.kind = 'TURN'
  AND (turn.spec ->> 'attempt')::integer = attempt.attempt;

WITH candidates AS (
    SELECT attempt.turn_id,
           attempt.attempt,
           revision.id AS runtime_revision_id,
           revision.version AS runtime_revision_version,
           row_number() OVER (
               PARTITION BY attempt.turn_id, attempt.attempt
               ORDER BY revision.created_at DESC, revision.id DESC
           ) AS rank,
           count(*) OVER (
               PARTITION BY attempt.turn_id, attempt.attempt
           ) AS candidate_count
    FROM control_plane.turn_attempts AS attempt
    JOIN control_plane.resources AS turn
      ON turn.id = attempt.turn_id
     AND turn.kind = 'TURN'
    JOIN control_plane.resources AS revision
      ON revision.organization_id = turn.organization_id
     AND revision.project_id = turn.project_id
     AND revision.owner_actor_id = turn.owner_actor_id
     AND revision.parent_id = (turn.spec ->> 'sessionId')::uuid
     AND revision.kind = 'RUNTIME_REVISION'
     AND revision.created_at <= attempt.started_at
     AND revision.created_at > coalesce((
         SELECT max(previous.started_at)
         FROM control_plane.turn_attempts AS previous
         WHERE previous.turn_id = attempt.turn_id
           AND previous.attempt < attempt.attempt
     ), '-infinity'::timestamptz)
    WHERE attempt.runtime_revision_id IS NULL
)
UPDATE control_plane.turn_attempts AS attempt
SET runtime_revision_id = candidate.runtime_revision_id,
    runtime_revision_version = candidate.runtime_revision_version
FROM candidates AS candidate
WHERE candidate.rank = 1
  AND candidate.candidate_count = 1
  AND candidate.turn_id = attempt.turn_id
  AND candidate.attempt = attempt.attempt;

-- +goose StatementBegin
DO $turn_attempt_revision_backfill$
BEGIN
    IF EXISTS (
        SELECT 1 FROM control_plane.turn_attempts
        WHERE runtime_revision_id IS NULL OR runtime_revision_version IS NULL
    ) THEN
        RAISE EXCEPTION 'turn attempt runtime revision backfill is incomplete'
            USING ERRCODE = '23514';
    END IF;
END
$turn_attempt_revision_backfill$;
-- +goose StatementEnd

ALTER TABLE control_plane.turn_attempts
    ALTER COLUMN runtime_revision_id SET NOT NULL,
    ALTER COLUMN runtime_revision_version SET NOT NULL,
    ADD CONSTRAINT turn_attempts_runtime_revision_fk
        FOREIGN KEY (runtime_revision_id) REFERENCES control_plane.resources(id)
        ON DELETE RESTRICT;

ALTER TABLE control_plane.runtime_execution_incidents
    ADD COLUMN version bigint NOT NULL DEFAULT 1
        CHECK (version BETWEEN 1 AND 9007199254740991),
    ADD COLUMN state text NOT NULL DEFAULT 'OPEN'
        CHECK (state IN ('OPEN', 'ACKNOWLEDGED', 'RETRYING', 'RELEASED', 'CLOSED')),
    ADD COLUMN action_reason_code text,
    ADD COLUMN updated_at timestamptz;
UPDATE control_plane.runtime_execution_incidents
SET updated_at = occurred_at
WHERE updated_at IS NULL;
ALTER TABLE control_plane.runtime_execution_incidents
    ALTER COLUMN updated_at SET NOT NULL,
    ADD CONSTRAINT runtime_execution_incidents_reason_v2_ck CHECK (
        (state = 'OPEN' AND action_reason_code IS NULL)
        OR (state <> 'OPEN' AND action_reason_code ~ '^[a-z][a-z0-9._-]{0,127}$')
    ),
    ADD CONSTRAINT runtime_execution_incidents_updated_v2_ck
        CHECK (updated_at >= occurred_at);

CREATE TABLE control_plane.runtime_incident_history (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    incident_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
    execution_fence bigint NOT NULL CHECK (
        execution_fence BETWEEN 1 AND 9007199254740991
    ),
    owner_actor_id uuid NOT NULL,
    state text NOT NULL CHECK (state IN (
        'OPEN', 'ACKNOWLEDGED', 'RETRYING', 'RELEASED', 'CLOSED'
    )),
    action text NOT NULL CHECK (action IN (
        'record', 'acknowledge', 'retry', 'release', 'close'
    )),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9._-]{0,127}$'),
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, incident_id, version),
    FOREIGN KEY (incident_id)
        REFERENCES control_plane.runtime_execution_incidents (id)
);
CREATE INDEX runtime_incident_history_owner_page_idx
    ON control_plane.runtime_incident_history (
        organization_id, project_id, owner_actor_id, incident_id, version DESC
    );
INSERT INTO control_plane.runtime_incident_history (
    organization_id, project_id, incident_id, version, execution_fence, owner_actor_id,
    state, action, reason_code, occurred_at
)
SELECT incident.organization_id, incident.project_id, incident.id,
    incident.version, incident.execution_fence, process.owner_actor_id, incident.state,
    'record', 'incident_recorded', incident.occurred_at
FROM control_plane.runtime_execution_incidents AS incident
JOIN control_plane.runtime_executions AS execution
  ON execution.organization_id = incident.organization_id
 AND execution.project_id = incident.project_id
 AND execution.id = incident.execution_id
JOIN control_plane.resources AS process
  ON process.organization_id = execution.organization_id
 AND process.project_id = execution.project_id
 AND process.id = execution.process_id
 AND process.kind = 'PROCESS_RUN';
ALTER TABLE control_plane.runtime_incident_history OWNER TO control_plane_owner;
ALTER TABLE control_plane.runtime_incident_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.runtime_incident_history FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_incident_history_runtime_scope
    ON control_plane.runtime_incident_history
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND (
            owner_actor_id = (control_plane.runtime_scope()).actor_id
            OR EXISTS (
                SELECT 1
                FROM control_plane.project_actor_permissions AS permission
                WHERE permission.organization_id = runtime_incident_history.organization_id
                  AND permission.project_id = runtime_incident_history.project_id
                  AND permission.actor_id = (control_plane.runtime_scope()).actor_id
                  AND permission.permission IN (
                      'controlplane.runtime_execution.incident.read',
                      'controlplane.runtime_execution.incident.manage'
                  )
            )
        )
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND (
            owner_actor_id = (control_plane.runtime_scope()).actor_id
            OR EXISTS (
                SELECT 1
                FROM control_plane.project_actor_permissions AS permission
                WHERE permission.organization_id = runtime_incident_history.organization_id
                  AND permission.project_id = runtime_incident_history.project_id
                  AND permission.actor_id = (control_plane.runtime_scope()).actor_id
                  AND permission.permission IN (
                      'controlplane.runtime_execution.incident.manage'
                  )
            )
        )
    );
REVOKE ALL ON control_plane.runtime_incident_history FROM PUBLIC;
GRANT SELECT, INSERT ON control_plane.runtime_incident_history TO control_plane_runtime;
GRANT UPDATE ON control_plane.runtime_execution_incidents TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.reject_legacy_configuration_mutation()
RETURNS trigger
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.kind IN ('ROLE', 'PROMPT_PROFILE') THEN
            RAISE EXCEPTION 'legacy ROLE and PROMPT_PROFILE configuration is immutable'
                USING ERRCODE = '0A000';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.kind IN ('ROLE', 'PROMPT_PROFILE') THEN
            RAISE EXCEPTION 'legacy ROLE and PROMPT_PROFILE configuration is immutable'
                USING ERRCODE = '0A000';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.kind IN ('ROLE', 'PROMPT_PROFILE')
       OR OLD.kind IN ('ROLE', 'PROMPT_PROFILE') THEN
        RAISE EXCEPTION 'legacy ROLE and PROMPT_PROFILE configuration is immutable'
            USING ERRCODE = '0A000';
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.reject_legacy_configuration_mutation() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.reject_legacy_configuration_mutation()
    TO control_plane_runtime;
CREATE TRIGGER resources_legacy_configuration_freeze
    BEFORE INSERT OR UPDATE OR DELETE ON control_plane.resources
    FOR EACH ROW
    EXECUTE FUNCTION control_plane.reject_legacy_configuration_mutation();

-- Organization-wide recovery остаётся одной physical transaction. Только
-- runtime adapter, владеющий HMAC key, может переключить exact Project scope;
-- organization, actor, session_user и generation обязаны совпасть с уже
-- активированным контекстом текущей транзакции.
-- +goose StatementBegin
CREATE FUNCTION control_plane.switch_runtime_workspace_context(
    requested_organization_id uuid,
    requested_project_id uuid,
    requested_actor_id uuid,
    requested_principal_name name,
    requested_principal_generation bigint,
    requested_key_id text,
    requested_nonce uuid,
    requested_expires_unix_micro bigint,
    requested_signature bytea
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    PERFORM 1
      FROM control_plane.runtime_transaction_contexts AS current_context
     WHERE current_context.backend_pid = pg_backend_pid()
       AND current_context.transaction_id = txid_current()
       AND current_context.principal_name = requested_principal_name
       AND current_context.principal_generation = requested_principal_generation
       AND current_context.organization_id = requested_organization_id
       AND current_context.actor_id = requested_actor_id
       AND current_context.expires_at > clock_timestamp()
     FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'workspace recovery context is invalid'
            USING ERRCODE = '28000';
    END IF;

    DELETE FROM control_plane.runtime_transaction_contexts
     WHERE backend_pid = pg_backend_pid()
       AND transaction_id = txid_current();

    PERFORM control_plane.activate_runtime_context(
        requested_organization_id,
        requested_project_id,
        requested_actor_id,
        requested_principal_name,
        requested_principal_generation,
        requested_key_id,
        requested_nonce,
        requested_expires_unix_micro,
        requested_signature
    );
END
$function$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.switch_runtime_workspace_context(
    uuid, uuid, uuid, name, bigint, text, uuid, bigint, bytea
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.switch_runtime_workspace_context(
    uuid, uuid, uuid, name, bigint, text, uuid, bigint, bytea
) TO control_plane_runtime;

-- Candidate discovery не выполняет effect и не держит lock между
-- транзакциями. Exact version/attempt/generation повторно проверяет terminal
-- command под owner locks; поэтому гонка всегда закрывается OCC-отказом.
-- +goose StatementBegin
CREATE FUNCTION control_plane.next_workspace_recovery_candidate()
RETURNS TABLE (
    organization_id uuid,
    project_id uuid,
    owner_actor_id uuid,
    resource_id uuid,
    kind text,
    version bigint,
    attempt integer,
    generation bigint,
    outcome text,
    terminal_reason_code text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_runtime', 'member') THEN
        RAISE EXCEPTION 'workspace recovery caller is invalid'
            USING ERRCODE = '28000';
    END IF;
    RETURN QUERY
    WITH backup_candidates AS (
        SELECT backup.organization_id,
               backup.project_id,
               backup.owner_actor_id,
               backup.id AS resource_id,
               backup.kind,
               backup.version,
               (backup.spec->>'attempt')::integer AS attempt,
               (backup.spec->>'generation')::bigint AS generation,
               CASE
                   WHEN backup.state = 'RUNNING'
                        AND backup.spec->>'backupState' = 'VERIFYING'
                       THEN 'complete'
                   WHEN backup.state = 'SUCCEEDED'
                        AND backup.spec->>'backupState' = 'AVAILABLE'
                        AND (backup.spec->>'retainUntil')::timestamptz <= clock_timestamp()
                       THEN 'expire'
               END AS outcome,
               CASE
                   WHEN backup.state = 'SUCCEEDED'
                        AND (backup.spec->>'retainUntil')::timestamptz <= clock_timestamp()
                       THEN 'backup_retention_expired'
                   ELSE ''
               END AS terminal_reason_code,
               backup.updated_at
          FROM control_plane.resources AS backup
         WHERE backup.kind = 'WORKSPACE_BACKUP'
           AND backup.state IN ('RUNNING', 'SUCCEEDED')
    ),
    restore_candidates AS (
        SELECT restore.organization_id,
               restore.project_id,
               restore.owner_actor_id,
               restore.id AS resource_id,
               restore.kind,
               restore.version,
               (restore.spec->>'attempt')::integer AS attempt,
               (restore.spec->>'generation')::bigint AS generation,
               CASE
                   WHEN (backup.spec->>'retainUntil')::timestamptz <= clock_timestamp()
                       THEN 'expire'
                   WHEN member_state.member_count > 0
                        AND member_state.succeeded_count = member_state.member_count
                       THEN 'complete'
                   WHEN member_state.failed_count > 0
                       THEN 'fail'
               END AS outcome,
               CASE
                   WHEN (backup.spec->>'retainUntil')::timestamptz <= clock_timestamp()
                       THEN 'backup_retention_expired'
                   WHEN member_state.failed_count > 0
                       THEN 'restore_member_failed'
                   ELSE ''
               END AS terminal_reason_code,
               restore.updated_at
          FROM control_plane.resources AS restore
          JOIN control_plane.resources AS backup
            ON backup.organization_id = restore.organization_id
           AND backup.project_id = restore.project_id
           AND backup.id = (restore.spec->>'backupId')::uuid
           AND backup.kind = 'WORKSPACE_BACKUP'
          CROSS JOIN LATERAL (
              SELECT count(*)::bigint AS member_count,
                     count(*) FILTER (
                         WHERE turn.state = 'SUCCEEDED'
                           AND execution.state = 'SUCCEEDED'
                           AND execution.turn_id = turn.id
                     )::bigint AS succeeded_count,
                     count(*) FILTER (
                         WHERE turn.state IN ('FAILED', 'CANCELLED', 'EXPIRED')
                            OR execution.state IN ('FAILED', 'CANCELLED', 'EXPIRED')
                     )::bigint AS failed_count
                FROM jsonb_array_elements(restore.spec->'members') AS member(value)
                LEFT JOIN control_plane.resources AS turn
                  ON turn.organization_id = restore.organization_id
                 AND turn.project_id = (member.value->>'workspaceId')::uuid
                 AND turn.id = (member.value->>'targetTurnId')::uuid
                 AND turn.kind = 'TURN'
                LEFT JOIN control_plane.runtime_executions AS execution
                  ON execution.organization_id = restore.organization_id
                 AND execution.project_id = (member.value->>'workspaceId')::uuid
                 AND execution.turn_id = (member.value->>'targetTurnId')::uuid
                 AND execution.attempt = (member.value->>'targetAttempt')::integer
          ) AS member_state
         WHERE restore.kind = 'WORKSPACE_RESTORE'
           AND restore.state IN ('QUEUED', 'RUNNING')
    ),
    candidates AS (
        SELECT * FROM backup_candidates WHERE backup_candidates.outcome IS NOT NULL
        UNION ALL
        SELECT * FROM restore_candidates WHERE restore_candidates.outcome IS NOT NULL
    )
    SELECT candidate.organization_id,
           candidate.project_id,
           candidate.owner_actor_id,
           candidate.resource_id,
           candidate.kind,
           candidate.version,
           candidate.attempt,
           candidate.generation,
           candidate.outcome,
           candidate.terminal_reason_code
      FROM candidates AS candidate
     ORDER BY candidate.updated_at, candidate.resource_id
     LIMIT 1;
END
$function$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.next_workspace_recovery_candidate() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.next_workspace_recovery_candidate()
    TO control_plane_runtime;

UPDATE control_plane.schema_state
SET version = 20260806023400, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260806023400 is forward-only: protected owner lifecycle and history cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
