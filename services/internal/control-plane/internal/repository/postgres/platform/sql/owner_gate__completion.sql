-- name: owner_gate__completion :one
WITH selected AS MATERIALIZED (
    SELECT gate.node_id,gate.root_run_id,gate.id
    FROM control_plane.owner_gates gate
    WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@gate_ref
), producer AS MATERIALIZED (
    SELECT edge.source_node_id
    FROM control_plane.run_edges edge JOIN selected ON selected.node_id=edge.target_node_id
    WHERE edge.organization_id=@organization_id::uuid AND edge.root_run_id=selected.root_run_id
        AND edge.type='WAITING_FOR'
)
SELECT root.id::text,root.state,COALESCE(version.spec,'null'::jsonb),
       COALESCE(root.workflow_exception,'null'::jsonb),
       COALESCE((SELECT source_node_id::text FROM producer),''),
       EXISTS(SELECT 1 FROM control_plane.owner_gates other
           WHERE other.organization_id=@organization_id::uuid AND other.root_run_id=root.id
               AND other.id<>selected.id AND other.state='OPEN'),
       EXISTS(SELECT 1 FROM control_plane.owner_gates other
           JOIN control_plane.run_edges edge ON edge.organization_id=other.organization_id
               AND edge.root_run_id=other.root_run_id AND edge.target_node_id=other.node_id
               AND edge.type='WAITING_FOR'
           WHERE other.organization_id=@organization_id::uuid AND other.root_run_id=root.id
               AND other.id<>selected.id AND other.state<>'APPROVED'
               AND edge.source_node_id IN (SELECT source_node_id FROM producer))
FROM selected JOIN control_plane.runs root ON root.id=selected.root_run_id
    AND root.organization_id=@organization_id::uuid AND root.root_run_id=root.id
LEFT JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id
    AND version.organization_id=root.organization_id;
