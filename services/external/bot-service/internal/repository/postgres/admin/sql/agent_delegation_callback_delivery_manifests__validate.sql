-- name: agent_delegation_callback_delivery_manifests__validate :one
select
	matter_codex_agent_delegation_callback_plan_valid($1, $2),
	expected_count,
	expected_plan,
	plan_sha256
from matter_codex_agent_delegation_callback_delivery_manifests
where delegation_id = $1 and callback_run_id = $2;
