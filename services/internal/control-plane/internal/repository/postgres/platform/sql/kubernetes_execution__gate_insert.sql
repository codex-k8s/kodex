-- name: kubernetes_execution__gate_insert :one
INSERT INTO control_plane.owner_gates(ref,organization_id,project_id,root_run_id,node_id,title,prompt,context_summary,allowed_decisions,state,integration_invocation_id,kubernetes_child_id)
SELECT @gate_ref,invocation.organization_id,root.project_id,root.id,@gate_node_id::uuid,
 'i18n:INTEGRATION_EFFECT_GATE_TITLE','i18n:INTEGRATION_EFFECT_GATE_PROMPT',@context_summary,
 ARRAY['APPROVE','REJECT','CANCEL'],'OPEN',invocation.id,@child_id::uuid
FROM control_plane.integration_invocations invocation
JOIN control_plane.runs current_run ON current_run.id=invocation.run_id AND current_run.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=current_run.root_run_id AND root.organization_id=invocation.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid
RETURNING id::text;
