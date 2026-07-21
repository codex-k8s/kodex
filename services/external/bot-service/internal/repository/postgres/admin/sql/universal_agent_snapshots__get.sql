-- name: universal_agent_snapshots__get :one
select
	role_definition.id, role_definition.organization_scope, role_definition.legacy_agent_role_id,
	role_definition.name, role_definition.slug, role_definition.role_type,
	role_definition.description, role_definition.default_policy::text, role_definition.status,
	role_definition.managed_by, role_definition.source_revision, role_definition.provenance::text,
	role_definition.record_version, role_definition.created_at, role_definition.updated_at,
	agent.id, agent.organization_scope, agent.legacy_agent_role_id,
	agent.role_definition_id, coalesce(agent.instruction_set_id, 0), coalesce(agent.bot_identity_id, 0),
	agent.name, agent.slug, agent.status, agent.managed_by, agent.source_revision,
	agent.provenance::text, agent.record_version, agent.created_at, agent.updated_at,
	(instruction_set.id is not null),
	coalesce(instruction_set.id, 0), coalesce(instruction_set.organization_scope, ''),
	coalesce(instruction_set.name, ''), coalesce(instruction_set.slug, ''),
	coalesce(instruction_set.source_type, ''), coalesce(instruction_set.managed_by, 'ui'),
	coalesce(instruction_set.source_revision, ''), coalesce(instruction_set.provenance::text, '{}'),
	coalesce(instruction_set.current_version_id, 0), coalesce(instruction_set.status, ''),
	coalesce(instruction_set.record_version, 0), coalesce(instruction_set.created_at, 'epoch'::timestamptz),
	coalesce(instruction_set.updated_at, 'epoch'::timestamptz),
	coalesce(version.id, 0), coalesce(version.organization_scope, ''),
	coalesce(version.instruction_set_id, 0), coalesce(version.version, 0),
	coalesce(version.markdown, ''), coalesce(version.content_sha256, ''::bytea),
	coalesce(version.actor_ref, ''), coalesce(version.created_at, 'epoch'::timestamptz)
from matter_codex_agents agent
join matter_codex_role_definitions role_definition on role_definition.id = agent.role_definition_id
left join matter_codex_instruction_sets instruction_set on instruction_set.id = agent.instruction_set_id
left join matter_codex_instruction_versions version on version.id = instruction_set.current_version_id
where agent.legacy_agent_role_id = $1;
