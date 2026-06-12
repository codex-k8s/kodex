-- name: openai_accounts__delete :one
with deleted_account as (
	delete from matter_codex_openai_accounts
	where name = $1
	returning id, name, credential_id, status, model_policy, created_at, updated_at
),
credential as (
	select c.id, c.secret_ref
	from matter_codex_credentials c
	join deleted_account a on a.credential_id = c.id
),
deleted_credential as (
	delete from matter_codex_credentials c
	using deleted_account a
	where c.id = a.credential_id
	returning c.id
)
select
	a.id,
	a.name,
	coalesce(c.id, 0) as credential_id,
	coalesce(c.secret_ref, '') as secret_ref,
	a.status,
	a.model_policy,
	a.created_at,
	a.updated_at
from deleted_account a
left join credential c on c.id = a.credential_id;
