-- name: agent_delegation_callback_delivery_manifests__get :one
select expected_count, expected_plan, plan_sha256
from matter_codex_agent_delegation_callback_delivery_manifests
where delegation_id = $1 and callback_run_id = $2;
