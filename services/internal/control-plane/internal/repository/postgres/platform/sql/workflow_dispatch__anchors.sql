-- name: workflow_dispatch__anchors :many
SELECT anchor.id::text,anchor.ref,anchor.workflow_step_key,anchor.state,
       anchor.workflow_business_attempt,anchor.workflow_attempts,
       CASE WHEN version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
           THEN latest_inputs.value ELSE anchor.workflow_inputs END,
       COALESCE((SELECT max(physical.workflow_business_attempt) FROM control_plane.run_nodes physical
           WHERE physical.organization_id=anchor.organization_id AND physical.root_run_id=anchor.root_run_id
               AND COALESCE(physical.workflow_step_origin_node_id,physical.id)=anchor.id),0),
       EXISTS(SELECT 1 FROM control_plane.owner_gates gate
           JOIN control_plane.run_edges edge ON edge.organization_id=gate.organization_id
               AND edge.root_run_id=gate.root_run_id AND edge.target_node_id=gate.node_id AND edge.type='WAITING_FOR'
           WHERE gate.organization_id=anchor.organization_id AND gate.root_run_id=anchor.root_run_id
               AND edge.source_node_id::text=anchor.workflow_attempts->-1->'provenance'->>'nodeId'
               AND gate.state='APPROVED'),
       EXISTS(SELECT 1 FROM control_plane.owner_gates gate
           JOIN control_plane.run_edges edge ON edge.organization_id=gate.organization_id
               AND edge.root_run_id=gate.root_run_id AND edge.target_node_id=gate.node_id AND edge.type='WAITING_FOR'
           WHERE gate.organization_id=anchor.organization_id AND gate.root_run_id=anchor.root_run_id
               AND edge.source_node_id::text=anchor.workflow_attempts->-1->'provenance'->>'nodeId'
               AND gate.state<>'APPROVED'),
       control_plane.workflow_irreversible_effect_exists(anchor.organization_id,anchor.root_run_id,anchor.id)
FROM control_plane.run_nodes anchor
JOIN control_plane.runs root ON root.id=anchor.root_run_id AND root.organization_id=anchor.organization_id
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
LEFT JOIN LATERAL (
    SELECT jsonb_agg(DISTINCT physical.workflow_inputs)->0 AS value
    FROM control_plane.run_nodes physical
    WHERE physical.organization_id=anchor.organization_id AND physical.root_run_id=anchor.root_run_id
        AND COALESCE(physical.workflow_step_origin_node_id,physical.id)=anchor.id
        AND physical.workflow_business_attempt=(SELECT max(latest.workflow_business_attempt)
            FROM control_plane.run_nodes latest WHERE latest.organization_id=anchor.organization_id
                AND latest.root_run_id=anchor.root_run_id AND COALESCE(latest.workflow_step_origin_node_id,latest.id)=anchor.id)
    HAVING count(DISTINCT physical.workflow_inputs)=1
) latest_inputs ON true
WHERE anchor.organization_id=@organization_id::uuid AND anchor.root_run_id=@root_run_id::uuid
    AND anchor.type='AGENT_EXECUTION' AND anchor.workflow_step_origin_node_id IS NULL
    AND (version.spec->>'ReworkPolicy' IS DISTINCT FROM 'OWNER_APPROVED_UPSTREAM_V1' OR latest_inputs.value IS NOT NULL)
    AND anchor.workflow_step_key<>'' AND anchor.workflow_step_key NOT LIKE 'workflow.coordinator.%'
ORDER BY anchor.workflow_step_key,anchor.id;
