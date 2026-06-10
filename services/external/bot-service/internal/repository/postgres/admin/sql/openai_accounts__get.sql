-- name: openai_accounts__get :one
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
where a.name = $1;
