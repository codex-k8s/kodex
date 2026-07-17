-- name: interaction_capabilities__cleanup :execrows
with candidates as (
	select token_hash
	from matter_codex_interaction_capabilities
	where expires_at < $1
		and status in ('unused', 'consumed', 'revoked')
	order by expires_at, token_hash
	limit $2
	for update skip locked
)
delete from matter_codex_interaction_capabilities capability
using candidates
where capability.token_hash = candidates.token_hash;
