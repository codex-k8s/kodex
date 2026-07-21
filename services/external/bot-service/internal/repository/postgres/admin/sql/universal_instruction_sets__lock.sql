-- name: universal_instruction_sets__lock :one
select id, managed_by, record_version
from matter_codex_instruction_sets
where organization_scope = 'installation'
	and slug = 'agent-' || $1::bigint::text
for update;
