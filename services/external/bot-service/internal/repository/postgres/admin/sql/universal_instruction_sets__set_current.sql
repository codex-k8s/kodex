-- name: universal_instruction_sets__set_current :exec
update matter_codex_instruction_sets
set current_version_id = $2,
	name = $3,
	status = $4,
	source_revision = $5,
	record_version = record_version + 1,
	updated_at = now()
where id = $1
	and managed_by = 'ui';
