-- name: automation_history__list :many
select
	run.public_id,
	run.status,
	run.outcome,
	coalesce(attention.id, 0),
	coalesce(attention.status, ''),
	case
		when attention.id is null then 'not_required'
		when attention.mattermost_post_id = '' then 'pending'
		else 'delivered'
	end,
	case
		when run.status = 'waiting_owner' and attention.mattermost_post_id = '' then 'retry_same_callback'
		when run.status = 'waiting_owner' then 'wait_for_owner_response'
		else 'none'
	end,
	greatest(run.updated_at, coalesce(attention.updated_at, run.updated_at))
from matter_codex_scheduled_runs run
left join matter_codex_owner_attention_requests attention
	on attention.automation_scheduled_run_id = run.id
	and attention.request_kind = 'automation'
where lower(run.owner_mattermost_user_name) = lower($1)
order by greatest(run.updated_at, coalesce(attention.updated_at, run.updated_at)) desc, run.id desc
limit $2;
