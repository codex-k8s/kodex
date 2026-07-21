-- name: universal_instruction_sets__insert :one
insert into matter_codex_instruction_sets(
	organization_scope, name, slug, source_type, managed_by, source_revision,
	status, record_version
) values (
	'installation', $2, 'agent-' || $1::bigint::text, 'ui_markdown', 'ui',
	'legacy-agent-role:' || $1::bigint::text, $3, 1
)
returning id;
