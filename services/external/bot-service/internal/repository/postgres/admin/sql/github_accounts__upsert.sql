-- name: github_accounts__upsert :one
with upserted_credential as (
	insert into matter_codex_credentials(name, credential_type, provider, secret_ref, status)
	values ($2, 'github_token', 'github', $3, $6)
	on conflict (name) do update set
		secret_ref = excluded.secret_ref,
		status = excluded.status,
		updated_at = now()
	returning id, secret_ref
),
upserted_account as (
	insert into matter_codex_github_accounts(name, credential_id, secret_ref, username, email, status)
	values ($1, (select id from upserted_credential), $3, $4, $5, $6)
	on conflict (name) do update set
		credential_id = excluded.credential_id,
		secret_ref = excluded.secret_ref,
		username = excluded.username,
		email = excluded.email,
		status = excluded.status,
		updated_at = now()
	returning id, name, credential_id, secret_ref, username, email, status, created_at, updated_at, (xmax = 0) as created
)
select
	a.id,
	a.name,
	a.credential_id,
	a.secret_ref,
	a.username,
	a.email,
	a.status,
	a.created_at,
	a.updated_at,
	a.created
from upserted_account a;
