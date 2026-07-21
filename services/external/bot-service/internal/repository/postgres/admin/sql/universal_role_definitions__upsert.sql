-- name: universal_role_definitions__upsert :one
insert into matter_codex_role_definitions(
	organization_scope, legacy_agent_role_id, name, slug, role_type, description,
	default_policy, status, managed_by, source_revision, record_version
) values (
	'installation', $1::bigint, $2, 'legacy-role-' || $1::bigint::text, $3, $4,
	jsonb_build_object('prompt_mode', $5::text), $6, 'ui',
	'legacy-agent-role:' || $1::bigint::text, 1
)
on conflict (legacy_agent_role_id) do update set
	name = excluded.name,
	role_type = excluded.role_type,
	description = excluded.description,
	default_policy = excluded.default_policy,
	status = excluded.status,
	source_revision = excluded.source_revision,
	record_version = matter_codex_role_definitions.record_version + 1,
	updated_at = now()
where matter_codex_role_definitions.managed_by = 'ui'
returning id;
