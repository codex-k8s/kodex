-- name: agent_delegation_callback_deliveries__claim :one
with candidate as (
	select delivery.id
	from matter_codex_agent_delegation_callback_deliveries delivery
	where delivery.delegation_id = $1
		and delivery.callback_run_id = $2
		and delivery.status <> 'delivered'
		and not (delivery.id = any($6::bigint[]))
		and (delivery.status <> 'in_flight' or delivery.lease_expires_at <= $3)
		and not exists (
			select 1
			from matter_codex_agent_delegation_callback_deliveries active
			where active.delegation_id = delivery.delegation_id
				and active.callback_run_id = delivery.callback_run_id
				and active.destination = delivery.destination
				and active.status = 'in_flight'
				and active.lease_expires_at > $3
		)
		and not exists (
			select 1
			from matter_codex_agent_delegation_callback_deliveries earlier
			where earlier.delegation_id = delivery.delegation_id
				and earlier.callback_run_id = delivery.callback_run_id
				and earlier.destination = delivery.destination
				and earlier.status <> 'delivered'
				and earlier.id < delivery.id
		)
	order by delivery.id
	for update skip locked
	limit 1
)
update matter_codex_agent_delegation_callback_deliveries delivery set
	status = 'in_flight',
	attempt_count = delivery.attempt_count + 1,
	lease_owner = $4,
	lease_expires_at = $5,
	last_attempt_at = $3,
	last_error_code = '',
	updated_at = $3
from candidate
where delivery.id = candidate.id
returning delivery.id, delivery.delegation_id, delivery.callback_run_id,
	delivery.destination, delivery.publication, delivery.channel_id,
	delivery.root_post_id, delivery.message, delivery.props,
	delivery.payload_sha256, delivery.external_id, delivery.status,
	delivery.attempt_count, coalesce(delivery.lease_owner, ''), delivery.lease_expires_at,
	delivery.last_attempt_at, delivery.last_error_code, delivery.mattermost_post_id,
	delivery.delivered_at, delivery.created_at, delivery.updated_at;
