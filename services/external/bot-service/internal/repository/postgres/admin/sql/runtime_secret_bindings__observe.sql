with changed as (
	insert into matter_codex_runtime_secret_binding_revisions (
		binding_key, secret_name, secret_key, integrity_sha256
	) values ($1, $2, $3, $4)
	on conflict (binding_key) do update set
		secret_name = excluded.secret_name,
		secret_key = excluded.secret_key,
		integrity_sha256 = excluded.integrity_sha256,
		revision = matter_codex_runtime_secret_binding_revisions.revision + 1,
		updated_at = now()
	where matter_codex_runtime_secret_binding_revisions.secret_name is distinct from excluded.secret_name
		or matter_codex_runtime_secret_binding_revisions.secret_key is distinct from excluded.secret_key
		or matter_codex_runtime_secret_binding_revisions.integrity_sha256 is distinct from excluded.integrity_sha256
	returning binding_key, secret_name, secret_key, integrity_sha256, revision, updated_at
), stable as (
	select binding_key, secret_name, secret_key, integrity_sha256, revision, updated_at
	from matter_codex_runtime_secret_binding_revisions
	where binding_key = $1
)
select * from changed
union all
select * from stable
limit 1;
-- name: runtime_secret_bindings__observe :one
