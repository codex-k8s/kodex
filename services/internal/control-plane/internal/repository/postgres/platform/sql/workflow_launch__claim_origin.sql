-- name: workflow_launch__claim_origin :one
WITH RECURSIVE origins AS (
 SELECT launch.*,1 AS depth FROM control_plane.required_workflow_launches launch WHERE launch.child_root_run_id=@root_run_id::uuid
 UNION ALL SELECT launch.*,origin.depth+1 FROM origins origin
 JOIN control_plane.required_workflow_launches launch ON launch.child_root_run_id=origin.origin_root_run_id WHERE origin.depth<9
)
SELECT NOT EXISTS(
 SELECT 1 FROM origins origin JOIN control_plane.runs parent ON parent.id=origin.origin_root_run_id
 JOIN control_plane.runs child ON child.id=origin.child_root_run_id
 JOIN control_plane.subjects actor ON actor.id=origin.root_actor_id
 JOIN control_plane.run_nodes node ON node.id=origin.origin_node_id
 JOIN control_plane.agents agent ON agent.id=node.agent_id
 WHERE origin.organization_id<>@organization_id::uuid OR origin.depth>8 OR origin.state<>'OPEN'
 OR parent.state NOT IN ('RUNNING','WAITING_HUMAN') OR NOT actor.active OR actor.kind<>'USER'
 OR parent.initiated_by<>origin.root_actor_id OR child.initiated_by<>origin.root_actor_id
 OR parent.project_id<>origin.project_id OR child.project_id<>origin.project_id
 OR NOT agent.enabled OR agent.state NOT IN ('READY','RUNNING') OR agent.system_key IS NOT NULL
 OR NOT ('platform.run.launch'=ANY(agent.capabilities)) OR agent.project_id<>origin.project_id
 OR child.workflow_version_id<>origin.workflow_version_id
);
