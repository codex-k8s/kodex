-- name: github_accounts__get :one
select id, name, credential_id, secret_ref, username, email, status, created_at, updated_at
from matter_codex_github_accounts
where name = $1;
