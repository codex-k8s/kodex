-- name: chat_repositories__list :many
select cr.id, cr.chat_id, cr.repository_id, r.provider, r.owner, r.name, cr.created_at
from matter_codex_chat_repositories cr
join matter_codex_repositories r on r.id = cr.repository_id
where cr.chat_id = $1
order by r.owner, r.name;
