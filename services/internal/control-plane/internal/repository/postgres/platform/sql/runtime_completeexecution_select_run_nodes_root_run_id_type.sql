-- name: runtime_completeexecution_select_run_nodes_root_run_id_type :one
SELECT (
    SELECT count(*)
    FROM control_plane.run_nodes
    WHERE root_run_id=$1::uuid
      AND type='AGENT_EXECUTION'
      AND state IN('QUEUED','RUNNING','WAITING')
) + (
    -- Ожидающие canonical invocations остаются owner work после завершения origin turn.
    SELECT count(*)
    FROM control_plane.integration_invocations invocation
    JOIN control_plane.run_nodes node ON node.id=invocation.node_id
      AND node.organization_id=invocation.organization_id AND node.run_id=invocation.run_id
    JOIN control_plane.runs execution ON execution.id=invocation.run_id
      AND execution.organization_id=invocation.organization_id AND execution.root_run_id=node.root_run_id
    JOIN control_plane.runs root ON root.id=execution.root_run_id
      AND root.organization_id=execution.organization_id AND root.root_run_id=root.id
    WHERE root.id=$1::uuid AND invocation.state IN ('READY','RUNNING')
) + CASE WHEN control_plane.legacy_rework_delivery_pending((SELECT organization_id FROM control_plane.runs WHERE id=$1::uuid),$1::uuid) THEN 1 ELSE 0 END + (
    SELECT count(*)
    FROM control_plane.owner_gates
    WHERE root_run_id=$1::uuid
      AND state='OPEN'
) + (
    SELECT count(*) FROM control_plane.required_workflow_launches
    WHERE origin_root_run_id=$1::uuid AND state='OPEN'
) + (
    SELECT count(*) FROM control_plane.repository_bundle_publications
    WHERE root_run_id=$1::uuid
      AND (state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME') OR continuation_state='BLOCKED')
), (
    SELECT count(*)
    FROM control_plane.run_nodes
    WHERE root_run_id=$1::uuid
      AND type='AGENT_EXECUTION'
      AND state='PLANNED'
      AND materialization_state='PLANNED'
)
