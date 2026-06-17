-- name: project_repositories__upsert :one
with demoted as (
	update matter_codex_project_repositories
	set is_default = false,
		updated_at = now()
	where project_id = $1
		and repository_id <> $2
		and $3::boolean = true
	returning id
), upserted as (
	insert into matter_codex_project_repositories(project_id, repository_id, is_default, metadata)
	values ($1, $2, $3, coalesce(nullif($4, '')::jsonb, '{}'::jsonb))
	on conflict (project_id, repository_id) do update set
		is_default = excluded.is_default,
		metadata = excluded.metadata,
		updated_at = now()
	returning id, project_id, repository_id, is_default, metadata::text, created_at, updated_at, (xmax = 0) as created
)
select u.id, u.project_id, u.repository_id, r.provider, r.owner, r.name, r.default_branch, u.is_default, u.metadata::text, u.created_at, u.updated_at, u.created
from upserted u
join matter_codex_repositories r on r.id = u.repository_id;
