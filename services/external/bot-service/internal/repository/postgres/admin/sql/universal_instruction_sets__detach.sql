-- name: universal_instruction_sets__detach :exec
update matter_codex_instruction_sets instruction_set
set managed_by = 'ui',
	source_type = 'ui_markdown',
	provenance = jsonb_build_object(
		'detached_from', 'git',
		'source_ref', instruction_set.source_ref,
		'source_revision', instruction_set.source_revision,
		'actor_ref', $2::text,
		'detached_at', now()
	),
	record_version = record_version + 1,
	updated_at = now()
from matter_codex_agents agent
where agent.instruction_set_id = instruction_set.id
	and agent.legacy_agent_role_id = $1
	and instruction_set.managed_by = 'git';
