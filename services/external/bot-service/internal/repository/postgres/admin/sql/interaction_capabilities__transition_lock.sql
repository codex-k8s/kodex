-- name: interaction_capabilities__transition_lock :many
select status
from matter_codex_interaction_capabilities
where token_hash = any($1::bytea[])
order by token_hash
for update
