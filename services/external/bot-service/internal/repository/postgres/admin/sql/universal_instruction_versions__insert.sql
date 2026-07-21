-- name: universal_instruction_versions__insert :one
insert into matter_codex_instruction_versions(
	organization_scope, instruction_set_id, version, markdown, content_sha256, actor_ref
)
select
	instruction_set.organization_scope,
	instruction_set.id,
	coalesce((select max(existing.version) from matter_codex_instruction_versions existing where existing.instruction_set_id = instruction_set.id), 0) + 1,
	$2,
	$3,
	$4
from matter_codex_instruction_sets instruction_set
where instruction_set.id = $1
returning id, version;
