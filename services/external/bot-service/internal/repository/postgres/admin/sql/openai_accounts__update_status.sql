-- name: openai_accounts__update_status :one
with updated_credential as (
	update matter_codex_credentials
	set
		secret_ref = case when $2 = '' then secret_ref else $2 end,
		status = $3,
		updated_at = now()
	where id = (
		select credential_id
		from matter_codex_openai_accounts
		where name = $1
	)
	returning id, secret_ref
),
updated_account as (
	update matter_codex_openai_accounts
	set
		status = $3,
		updated_at = now()
	where name = $1
	returning id, name, credential_id, status, model_policy, created_at, updated_at
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
from updated_account a
left join matter_codex_credentials c on c.id = a.credential_id;
