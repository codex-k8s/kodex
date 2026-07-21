-- name: universal_instruction_versions__current :one
select version.id, version.version, version.content_sha256
from matter_codex_instruction_sets instruction_set
join matter_codex_instruction_versions version
	on version.id = instruction_set.current_version_id
where instruction_set.id = $1;
