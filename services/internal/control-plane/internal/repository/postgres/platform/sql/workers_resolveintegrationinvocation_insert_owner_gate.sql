-- name: workers_resolveintegrationinvocation_insert_owner_gate :one
INSERT INTO control_plane.owner_gates(
	ref,organization_id,project_id,root_run_id,node_id,title,prompt,context_summary,
	allowed_decisions,state,integration_invocation_id
)
VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'i18n:INTEGRATION_EFFECT_GATE_TITLE',
	'i18n:INTEGRATION_EFFECT_GATE_PROMPT',$6,ARRAY['APPROVE','REJECT','CANCEL'],'OPEN',$7::uuid)
RETURNING id::text
