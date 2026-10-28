-- name: legacy_workflow_rework__parent :many
WITH RECURSIVE budget AS MATERIALIZED (
    SELECT count(*) AS nodes FROM control_plane.run_nodes
    WHERE organization_id=@organization_id::uuid AND root_run_id=@root_run_id::uuid
), lineage AS (
    SELECT parent.id,parent.run_id,parent.agent_id,ARRAY[parent.id] AS path,0 AS depth
    FROM control_plane.run_edges callback
    JOIN control_plane.run_nodes parent ON parent.id=callback.target_node_id AND parent.organization_id=callback.organization_id
        AND parent.root_run_id=callback.root_run_id
    WHERE callback.organization_id=@organization_id::uuid AND callback.root_run_id=@root_run_id::uuid
        AND callback.source_node_id=@source_node_id::uuid AND callback.type='CALLBACK_TO'
    UNION ALL
    SELECT next_node.id,next_node.run_id,next_node.agent_id,lineage.path||next_node.id,lineage.depth+1
    FROM lineage JOIN control_plane.run_edges edge ON edge.source_node_id=lineage.id
        AND edge.organization_id=@organization_id::uuid AND edge.root_run_id=@root_run_id::uuid AND edge.type='CONTINUES'
    JOIN control_plane.run_nodes next_node ON next_node.id=edge.target_node_id
        AND next_node.organization_id=@organization_id::uuid AND next_node.root_run_id=@root_run_id::uuid
        AND next_node.run_id=lineage.run_id AND next_node.agent_id=lineage.agent_id
    CROSS JOIN budget WHERE lineage.depth<budget.nodes AND NOT next_node.id=ANY(lineage.path)
)
SELECT parent.id::text,parent.ref,parent.run_id::text,parent.state,
    EXISTS (SELECT 1 FROM control_plane.runtime_revisions revision WHERE revision.node_id=parent.id),
    EXISTS (SELECT 1 FROM control_plane.runtime_leases lease WHERE lease.node_id=parent.id AND lease.state='CLAIMED'),agent.ref
FROM lineage JOIN control_plane.run_nodes parent ON parent.id=lineage.id
JOIN control_plane.runs root ON root.id=@root_run_id::uuid AND root.organization_id=parent.organization_id
    AND root.id=parent.run_id AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.agents agent ON agent.id=parent.agent_id AND agent.organization_id=root.organization_id
    AND agent.ref=version.spec->>'CoordinatorAgentRef' AND agent.enabled AND agent.state='READY'
WHERE NOT EXISTS (SELECT 1 FROM control_plane.run_edges future WHERE future.source_node_id=parent.id AND future.type='CONTINUES')
    AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges branch WHERE branch.source_node_id=ANY(lineage.path)
        AND branch.type='CONTINUES' GROUP BY branch.source_node_id HAVING count(*)>1)
FOR UPDATE OF parent NOWAIT;
