-- name: cluster_admin_openai_account__rotate :one
with rotated as (
	select *
	from matter_codex_rotate_frozen_openai_credential($1, $2, $3, $4, $5, $6, $7)
)
select
	account.id,
	account.name,
	coalesce(credential.id, 0) as credential_id,
	coalesce(credential.secret_ref, '') as secret_ref,
	account.status,
	account.model_policy,
	account.created_at,
	account.updated_at
from rotated account
left join matter_codex_credentials credential on credential.id = account.credential_id;
