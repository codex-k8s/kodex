-- name: openai_accounts__upsert :one
with upserted_credential as (
	insert into matter_codex_credentials(name, credential_type, provider, secret_ref, status)
	values ($2, 'codex_auth', 'openai', $3, $4)
	on conflict (name) do update set
		secret_ref = excluded.secret_ref,
		status = excluded.status,
		updated_at = now()
	returning id, secret_ref
),
upserted_account as (
	insert into matter_codex_openai_accounts(name, credential_id, status)
	values ($1, (select id from upserted_credential), $4)
	on conflict (name) do update set
		credential_id = excluded.credential_id,
		status = excluded.status,
		updated_at = now()
	returning id, name, credential_id, status, model_policy, created_at, updated_at, (xmax = 0) as created
)
select
	a.id,
	a.name,
	a.credential_id,
	c.secret_ref,
	a.status,
	a.model_policy,
	a.created_at,
	a.updated_at,
	a.created
from upserted_account a
join upserted_credential c on c.id = a.credential_id;
