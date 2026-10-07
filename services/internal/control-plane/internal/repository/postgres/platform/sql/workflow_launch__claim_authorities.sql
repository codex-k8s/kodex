-- name: workflow_launch__claim_authorities :many
WITH RECURSIVE origins AS (
 SELECT launch.*,1 AS depth FROM control_plane.required_workflow_launches launch WHERE launch.child_root_run_id=@root_run_id::uuid
 UNION ALL SELECT launch.*,origin.depth+1 FROM origins origin
 JOIN control_plane.required_workflow_launches launch ON launch.child_root_run_id=origin.origin_root_run_id WHERE origin.depth<9
)
SELECT actor.ref,organization.ref,project.id::text,project.ref,agent.ref,agent.capabilities,origin.workflow_ref
FROM origins origin
JOIN control_plane.subjects actor ON actor.id=origin.root_actor_id
JOIN control_plane.organizations organization ON organization.id=origin.organization_id
JOIN control_plane.projects project ON project.id=origin.project_id
JOIN control_plane.run_nodes node ON node.id=origin.origin_node_id
JOIN control_plane.agents agent ON agent.id=node.agent_id
WHERE origin.organization_id=@organization_id::uuid
ORDER BY origin.depth;
