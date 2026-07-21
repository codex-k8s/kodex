-- name: universal_instruction_sets__set_current :exec
update matter_codex_instruction_sets
set current_version_id = $2,
	name = $3,
	status = $4,
	source_revision = $5,
	record_version = record_version + case
		when (current_version_id, name, status, source_revision)
			is distinct from ($2::bigint, $3::text, $4::text, $5::text)
		then 1 else 0 end,
	updated_at = case
		when (current_version_id, name, status, source_revision)
			is distinct from ($2::bigint, $3::text, $4::text, $5::text)
		then now() else updated_at end
where id = $1
	and managed_by = 'ui'
	and record_version = $6;
