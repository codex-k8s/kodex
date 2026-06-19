-- name: project_runtime_variables__upsert :one
insert into matter_codex_project_runtime_variables(
	project_id,
	name,
	slug,
	description,
	secret_ref,
	secret_key,
	sensitive,
	enabled
) values (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8
)
on conflict (project_id, name) do update set
	slug = excluded.slug,
	description = excluded.description,
	secret_ref = excluded.secret_ref,
	secret_key = excluded.secret_key,
	sensitive = excluded.sensitive,
	enabled = excluded.enabled,
	updated_at = now()
returning id, project_id, name, slug, description, secret_ref, secret_key, sensitive, enabled, created_at, updated_at, (xmax = 0) as created;
