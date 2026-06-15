-- name: github_accounts__list :many
select id, name, credential_id, secret_ref, username, email, scopes, status, created_at, updated_at
from matter_codex_github_accounts
order by name
limit $1;
