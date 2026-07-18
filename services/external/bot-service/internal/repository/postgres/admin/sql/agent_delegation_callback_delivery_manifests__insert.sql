-- name: agent_delegation_callback_delivery_manifests__insert :one
insert into matter_codex_agent_delegation_callback_delivery_manifests(
	delegation_id, callback_run_id, expected_count, expected_plan, plan_sha256
) values ($1, $2, $3, $4::jsonb, $5)
on conflict (delegation_id, callback_run_id) do nothing
returning expected_count, expected_plan, plan_sha256;
