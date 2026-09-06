-- name: owner_gate_intent :one
SELECT COALESCE(source.ref,''),
       invocation.id IS NOT NULL,
       delivery.id IS NOT NULL,
       CASE WHEN gate.state='OPEN' THEN NOT EXISTS(SELECT 1 FROM control_plane.run_nodes active
         WHERE active.root_run_id=gate.root_run_id AND active.type='AGENT_EXECUTION'
           AND active.state IN ('QUEUED','RUNNING','WAITING'))
       WHEN NOT $5::boolean AND gate.state='APPROVED' THEN root.state='SUCCEEDED'
       ELSE EXISTS(SELECT 1 FROM control_plane.run_events resolved
         WHERE resolved.organization_id=gate.organization_id AND resolved.root_run_id=gate.root_run_id
           AND resolved.gate_ref=gate.ref AND resolved.type='OWNER_GATE_RESOLVED' AND resolved.run_state='SUCCEEDED') END,
       COALESCE(connection.ref,''),COALESCE(connection.name,''),COALESCE(connection.definition_key,''),
       COALESCE(invocation.capability_key,''),COALESCE(invocation.operation,''),
       COALESCE(invocation.effect_key,''),COALESCE(invocation.resource_kind,''),
       COALESCE(invocation.resource_scope,'{}'::jsonb),COALESCE(invocation.resource_scope_digest,''),
       COALESCE(invocation.bounded_input,'{}'::jsonb),COALESCE(invocation.input_digest,''),
       COALESCE(invocation.risk,''),COALESCE(invocation.approval_policy,'')
FROM control_plane.owner_gates gate
JOIN control_plane.runs root ON root.id=gate.root_run_id AND root.organization_id=gate.organization_id
JOIN control_plane.run_nodes node ON node.id=gate.node_id AND node.organization_id=gate.organization_id
LEFT JOIN control_plane.run_nodes predecessor ON predecessor.id=node.parent_node_id AND predecessor.organization_id=gate.organization_id
LEFT JOIN control_plane.session_turns turn ON turn.id=predecessor.turn_id AND turn.organization_id=gate.organization_id
LEFT JOIN control_plane.attachment_sets source ON source.id=turn.attachment_set_id
  AND source.organization_id=gate.organization_id AND source.project_id=gate.project_id AND source.state='FINALIZED'
LEFT JOIN control_plane.integration_invocations invocation ON invocation.id=gate.integration_invocation_id
  AND invocation.organization_id=gate.organization_id AND invocation.node_id=predecessor.id
LEFT JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
  AND connection.organization_id=gate.organization_id
LEFT JOIN control_plane.interaction_deliveries delivery ON delivery.approval_gate_id=gate.id
  AND delivery.organization_id=gate.organization_id AND delivery.project_id=gate.project_id
WHERE gate.organization_id=$1::uuid AND gate.ref=$2
  AND ($6='' OR gate.project_id::text=$6)
  AND (NOT $5::boolean OR $3 IN ('OWNER','ADMINISTRATOR') OR EXISTS (
    SELECT 1 FROM control_plane.memberships membership
    WHERE membership.project_id=gate.project_id AND membership.subject_id=$4::uuid
      AND membership.active AND 'VIEW'=ANY(membership.permissions)))
  AND (gate.integration_invocation_id IS NULL OR (invocation.id IS NOT NULL AND connection.id IS NOT NULL));
