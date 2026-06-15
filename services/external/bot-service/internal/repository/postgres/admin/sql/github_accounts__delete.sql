-- name: github_accounts__delete :one
delete from matter_codex_github_accounts
where name = $1
returning id, name, credential_id, secret_ref, username, email, scopes, status, created_at, updated_at;
