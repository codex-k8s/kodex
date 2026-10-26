-- name: workflow_dispatch__pending :one
WITH members AS MATERIALIZED (
    SELECT node.id,node.run_id,node.state,node.type FROM control_plane.run_nodes node
    WHERE node.organization_id=@organization_id::uuid AND node.root_run_id=@root_run_id::uuid
        AND (@origin_node_id::text='' OR (COALESCE(node.workflow_step_origin_node_id,node.id)=NULLIF(@origin_node_id,'')::uuid
            AND node.workflow_business_attempt=@business_attempt::integer))
)
SELECT EXISTS(SELECT 1 FROM members WHERE state IN ('QUEUED','RUNNING','WAITING') AND type='AGENT_EXECUTION')
    OR EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch JOIN members ON members.id=launch.origin_node_id
        WHERE launch.organization_id=@organization_id::uuid AND launch.state='OPEN')
    OR EXISTS(SELECT 1 FROM members WHERE control_plane.repository_bundle_callback_pending(@organization_id::uuid,members.run_id))
    OR EXISTS(SELECT 1 FROM control_plane.integration_invocations invocation JOIN members ON members.id=invocation.node_id
        WHERE invocation.organization_id=@organization_id::uuid AND invocation.state NOT IN ('SUCCEEDED','FAILED','REJECTED','CANCELLED'))
    OR EXISTS(SELECT 1 FROM control_plane.run_edges edge JOIN members ON members.id=edge.target_node_id
        WHERE edge.organization_id=@organization_id::uuid AND edge.root_run_id=@root_run_id::uuid AND edge.type='CALLBACK_TO'
            AND NOT EXISTS(SELECT 1 FROM control_plane.callback_receipts receipt WHERE receipt.callback_edge_id=edge.id));
