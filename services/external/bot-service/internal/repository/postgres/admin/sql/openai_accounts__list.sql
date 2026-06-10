-- name: openai_accounts__list :many
select
	a.id,
	a.name,
	coalesce(c.id, 0) as credential_id,
	coalesce(c.secret_ref, '') as secret_ref,
	a.status,
	a.model_policy,
	a.created_at,
	a.updated_at
from matter_codex_openai_accounts a
left join matter_codex_credentials c on c.id = a.credential_id
order by a.name
limit $1;
