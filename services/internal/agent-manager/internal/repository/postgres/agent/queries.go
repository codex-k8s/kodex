package agent

const flowColumns = `
    id,
    scope_type,
    scope_ref,
    slug,
    display_name,
    description,
    icon_object_uri,
    status,
    active_version_id,
    version,
    created_at,
    updated_at`

const flowVersionColumns = `
    id,
    flow_id,
    version,
    source_ref,
    definition_digest,
    status,
    activated_at,
    created_at`

const stageColumns = `
    id,
    flow_version_id,
    slug,
    stage_type,
    display_name,
    icon_object_uri,
    required_artifacts,
    acceptance_policy,
    position`

const roleColumns = `
    id,
    scope_type,
    scope_ref,
    slug,
    display_name,
    icon_object_uri,
    role_kind,
    runtime_profile,
    allowed_mcp_tools,
    provider_account_policy_ref,
    status,
    version,
    created_at,
    updated_at`

const promptTemplateColumns = `
    id,
    role_profile_id,
    prompt_kind,
    active_version_id,
    version,
    created_at,
    updated_at`

const promptVersionColumns = `
    id,
    prompt_template_id,
    role_profile_id,
    prompt_kind,
    version,
    source_ref,
    template_object_uri,
    template_object_digest,
    template_object_size_bytes,
    template_digest,
    status,
    activated_at,
    created_at`

const (
	queryFlowCreate = `
INSERT INTO agent_manager_flows (
    id, scope_type, scope_ref, slug, display_name, description, icon_object_uri,
    status, active_version_id, version, created_at, updated_at
) VALUES (
    @id, @scope_type, @scope_ref, @slug, @display_name, @description, @icon_object_uri,
    @status, @active_version_id::uuid, @version, @created_at, @updated_at
);`

	queryFlowUpdate = `
UPDATE agent_manager_flows
SET
    display_name = @display_name,
    description = @description,
    icon_object_uri = @icon_object_uri,
    status = @status,
    active_version_id = @active_version_id::uuid,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;`

	queryFlowGet = `SELECT ` + flowColumns + `
FROM agent_manager_flows
WHERE id = @id;`

	queryFlowList = `SELECT ` + flowColumns + `
FROM agent_manager_flows
WHERE (@scope_type::text IS NULL OR scope_type = @scope_type::text)
  AND (@scope_ref::text IS NULL OR scope_ref = @scope_ref::text)
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY scope_type, scope_ref, slug, id
LIMIT @limit::integer
OFFSET @offset::integer;`

	queryFlowVersionCreate = `
INSERT INTO agent_manager_flow_versions (
    id, flow_id, version, source_ref, definition_digest, status, activated_at, created_at
) VALUES (
    @id, @flow_id, @version, @source_ref, @definition_digest, @status, @activated_at::timestamptz, @created_at
);`

	queryFlowVersionActivate = `
UPDATE agent_manager_flow_versions
SET status = @status,
    activated_at = @activated_at::timestamptz
WHERE id = @id;`

	queryFlowVersionSupersede = `
UPDATE agent_manager_flow_versions
SET status = 'superseded'
WHERE flow_id = @flow_id
  AND id <> @id
  AND status = 'active';`

	queryFlowVersionGet = `SELECT ` + flowVersionColumns + `
FROM agent_manager_flow_versions
WHERE id = @id;`

	queryFlowVersionList = `SELECT ` + flowVersionColumns + `
FROM agent_manager_flow_versions
WHERE flow_id = @flow_id
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY version DESC, id
LIMIT @limit::integer
OFFSET @offset::integer;`

	queryStageCreate = `
INSERT INTO agent_manager_stages (
    id, flow_version_id, slug, stage_type, display_name, icon_object_uri,
    required_artifacts, acceptance_policy, position
) VALUES (
    @id, @flow_version_id, @slug, @stage_type, @display_name, @icon_object_uri,
    @required_artifacts, @acceptance_policy, @position
);`

	queryStageListByFlowVersion = `SELECT ` + stageColumns + `
FROM agent_manager_stages
WHERE flow_version_id = @flow_version_id
ORDER BY position, id;`

	queryStageTransitionCreate = `
INSERT INTO agent_manager_stage_transitions (
    id, flow_version_id, from_stage_id, to_stage_id, condition_payload, follow_up_type, position
) VALUES (
    @id, @flow_version_id, @from_stage_id::uuid, @to_stage_id, @condition_payload, @follow_up_type, @position
);`

	queryStageTransitionListByFlowVersion = `
SELECT
    id,
    flow_version_id,
    from_stage_id,
    to_stage_id,
    condition_payload,
    follow_up_type,
    position
FROM agent_manager_stage_transitions
WHERE flow_version_id = @flow_version_id
ORDER BY position, id;`

	queryStageRoleBindingCreate = `
INSERT INTO agent_manager_stage_role_bindings (
    id, stage_id, role_profile_id, binding_kind, launch_policy, required_for_acceptance
) VALUES (
    @id, @stage_id, @role_profile_id, @binding_kind, @launch_policy, @required_for_acceptance
);`

	queryStageRoleBindingListByFlowVersion = `
SELECT
    binding.id,
    binding.stage_id,
    binding.role_profile_id,
    binding.binding_kind,
    binding.launch_policy,
    binding.required_for_acceptance
FROM agent_manager_stage_role_bindings AS binding
JOIN agent_manager_stages AS stage ON stage.id = binding.stage_id
WHERE stage.flow_version_id = @flow_version_id
ORDER BY stage.position, binding.binding_kind, binding.id;`

	queryRoleCreate = `
INSERT INTO agent_manager_role_profiles (
    id, scope_type, scope_ref, slug, display_name, icon_object_uri, role_kind,
    runtime_profile, allowed_mcp_tools, provider_account_policy_ref, status,
    version, created_at, updated_at
) VALUES (
    @id, @scope_type, @scope_ref, @slug, @display_name, @icon_object_uri, @role_kind,
    @runtime_profile, @allowed_mcp_tools, @provider_account_policy_ref, @status,
    @version, @created_at, @updated_at
);`

	queryRoleUpdate = `
UPDATE agent_manager_role_profiles
SET
    display_name = @display_name,
    icon_object_uri = @icon_object_uri,
    role_kind = @role_kind,
    runtime_profile = @runtime_profile,
    allowed_mcp_tools = @allowed_mcp_tools,
    provider_account_policy_ref = @provider_account_policy_ref,
    status = @status,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;`

	queryRoleGet = `SELECT ` + roleColumns + `
FROM agent_manager_role_profiles
WHERE id = @id;`

	queryRoleList = `SELECT ` + roleColumns + `
FROM agent_manager_role_profiles
WHERE (@scope_type::text IS NULL OR scope_type = @scope_type::text)
  AND (@scope_ref::text IS NULL OR scope_ref = @scope_ref::text)
  AND (@role_kind::text IS NULL OR role_kind = @role_kind::text)
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY scope_type, scope_ref, slug, id
LIMIT @limit::integer
OFFSET @offset::integer;`

	queryPromptTemplateCreate = `
INSERT INTO agent_manager_prompt_templates (
    id, role_profile_id, prompt_kind, active_version_id, version, created_at, updated_at
) VALUES (
    @id, @role_profile_id, @prompt_kind, @active_version_id::uuid, @version, @created_at, @updated_at
);`

	queryPromptTemplateUpdate = `
UPDATE agent_manager_prompt_templates
SET
    active_version_id = @active_version_id::uuid,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;`

	queryPromptTemplateGet = `SELECT ` + promptTemplateColumns + `
FROM agent_manager_prompt_templates
WHERE id = @id;`

	queryPromptTemplateList = `SELECT ` + promptTemplateColumns + `
FROM agent_manager_prompt_templates
WHERE role_profile_id = @role_profile_id
  AND (@prompt_kind::text IS NULL OR prompt_kind = @prompt_kind::text)
ORDER BY prompt_kind, id
LIMIT @limit::integer
OFFSET @offset::integer;`

	queryPromptVersionCreate = `
INSERT INTO agent_manager_prompt_template_versions (
    id, prompt_template_id, role_profile_id, prompt_kind, version, source_ref,
    template_object_uri, template_object_digest, template_object_size_bytes,
    template_digest, status, activated_at, created_at
) VALUES (
    @id, @prompt_template_id, @role_profile_id, @prompt_kind, @version, @source_ref,
    @template_object_uri, @template_object_digest, @template_object_size_bytes,
    @template_digest, @status, @activated_at::timestamptz, @created_at
);`

	queryPromptVersionActivate = `
UPDATE agent_manager_prompt_template_versions
SET status = @status,
    activated_at = @activated_at::timestamptz
WHERE id = @id;`

	queryPromptVersionSupersede = `
UPDATE agent_manager_prompt_template_versions
SET status = 'superseded'
WHERE prompt_template_id = @prompt_template_id
  AND id <> @id
  AND status = 'active';`

	queryPromptVersionGet = `SELECT ` + promptVersionColumns + `
FROM agent_manager_prompt_template_versions
WHERE id = @id;`

	queryPromptVersionList = `SELECT ` + promptVersionColumns + `
FROM agent_manager_prompt_template_versions
WHERE role_profile_id = @role_profile_id
  AND (@prompt_kind::text IS NULL OR prompt_kind = @prompt_kind::text)
  AND (@status::text IS NULL OR status = @status::text)
ORDER BY prompt_kind, version DESC, id
LIMIT @limit::integer
OFFSET @offset::integer;`

	queryCommandResultCreate = `
INSERT INTO agent_manager_command_results (
    key, command_id, idempotency_key, operation, aggregate_type, aggregate_id,
    result_payload, created_at
) VALUES (
    @key, @command_id::uuid, @idempotency_key, @operation, @aggregate_type, @aggregate_id,
    @result_payload, @created_at
);`

	queryCommandResultGet = `
SELECT
    key,
    command_id,
    idempotency_key,
    operation,
    aggregate_type,
    aggregate_id,
    result_payload,
    created_at
FROM agent_manager_command_results
WHERE (@command_id::uuid IS NOT NULL AND command_id = @command_id::uuid)
   OR (@command_id::uuid IS NULL AND operation = @operation AND idempotency_key = @idempotency_key)
LIMIT 1;`

	queryOutboxEventCreate = `
INSERT INTO agent_manager_outbox_events (
    id, event_type, schema_version, aggregate_type, aggregate_id, payload, occurred_at, published_at
) VALUES (
    @id, @event_type, @schema_version, @aggregate_type, @aggregate_id, @payload, @occurred_at, @published_at::timestamptz
);`

	queryOutboxEventClaim = `
WITH selected AS (
    SELECT id
    FROM agent_manager_outbox_events
    WHERE published_at IS NULL
      AND failed_permanently_at IS NULL
      AND next_attempt_at <= @now
      AND (locked_until IS NULL OR locked_until <= @now)
    ORDER BY occurred_at, id
    LIMIT @limit::integer
    FOR UPDATE SKIP LOCKED
)
UPDATE agent_manager_outbox_events AS event
SET
    attempt_count = event.attempt_count + 1,
    locked_until = @locked_until,
    failure_kind = '',
    last_error = ''
FROM selected
WHERE event.id = selected.id
RETURNING
    event.id,
    event.event_type,
    event.schema_version,
    event.aggregate_type,
    event.aggregate_id,
    event.payload,
    event.occurred_at,
    event.published_at,
    event.attempt_count,
    event.next_attempt_at,
    event.locked_until,
    event.failed_permanently_at,
    event.failure_kind,
    event.last_error;`

	queryOutboxEventMarkPublished = `
UPDATE agent_manager_outbox_events
SET published_at = @published_at,
    locked_until = NULL,
    failure_kind = '',
    last_error = ''
WHERE id = @id
  AND attempt_count = @attempt_count;`

	queryOutboxEventMarkFailed = `
UPDATE agent_manager_outbox_events
SET locked_until = NULL,
    next_attempt_at = @next_attempt_at,
    failure_kind = 'transient',
    last_error = @last_error
WHERE id = @id
  AND attempt_count = @attempt_count;`

	queryOutboxEventMarkPermanent = `
UPDATE agent_manager_outbox_events
SET locked_until = NULL,
    failed_permanently_at = @failed_permanently_at,
    failure_kind = 'permanent',
    last_error = @last_error
WHERE id = @id
  AND attempt_count = @attempt_count;`
)
