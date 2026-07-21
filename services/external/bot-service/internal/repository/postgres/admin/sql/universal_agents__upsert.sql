-- name: universal_agents__upsert :one
insert into matter_codex_agents(
	organization_scope, legacy_agent_role_id, role_definition_id,
	instruction_set_id, bot_identity_id, name, slug, status, managed_by,
	source_revision, record_version
)
select
	role_definition.organization_scope,
	$1,
	role_definition.id,
	$2,
	bot_identity.id,
	$3,
	'legacy-agent-' || $1::text,
	$4,
	'ui',
	'legacy-agent-role:' || $1::text,
	1
from matter_codex_role_definitions role_definition
left join matter_codex_mattermost_bot_identities bot_identity on bot_identity.role_id = $1
where role_definition.legacy_agent_role_id = $1
on conflict (legacy_agent_role_id) do update set
	role_definition_id = excluded.role_definition_id,
	instruction_set_id = excluded.instruction_set_id,
	bot_identity_id = coalesce(excluded.bot_identity_id, matter_codex_agents.bot_identity_id),
	name = excluded.name,
	status = excluded.status,
	source_revision = excluded.source_revision,
	record_version = matter_codex_agents.record_version + 1,
	updated_at = now()
where matter_codex_agents.managed_by = 'ui'
returning id;
