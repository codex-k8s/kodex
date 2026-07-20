-- name: interaction_capabilities__transition :exec
update matter_codex_interaction_capabilities
set status = $3
where token_hash = any($1::bytea[])
	and status = $2
