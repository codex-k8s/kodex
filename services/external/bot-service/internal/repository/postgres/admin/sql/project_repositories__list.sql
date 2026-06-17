-- name: project_repositories__list :many
select pr.id, pr.project_id, pr.repository_id, r.provider, r.owner, r.name, r.default_branch, pr.is_default, pr.metadata::text, pr.created_at, pr.updated_at
from matter_codex_project_repositories pr
join matter_codex_repositories r on r.id = pr.repository_id
where pr.project_id = $1
order by pr.is_default desc, r.owner, r.name;
