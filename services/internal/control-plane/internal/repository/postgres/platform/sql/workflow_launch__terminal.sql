-- name: workflow_launch__terminal :many
SELECT launch.id::text,launch.origin_root_run_id::text,launch.origin_run_id::text,launch.origin_node_id::text,parent_node.ref,
 launch.project_id::text,child.id::text,child.ref,child.state,child.version,COALESCE(child.result_summary,''),parent.state,
 launch.proxy_node_id::text,proxy.ref,launch.callback_edge_id::text,edge.ref
FROM control_plane.required_workflow_launches launch
JOIN control_plane.runs parent ON parent.id=launch.origin_root_run_id
JOIN control_plane.run_nodes parent_node ON parent_node.id=launch.origin_node_id
JOIN control_plane.runs child ON child.id=launch.child_root_run_id
JOIN control_plane.run_nodes proxy ON proxy.id=launch.proxy_node_id
JOIN control_plane.run_edges edge ON edge.id=launch.callback_edge_id
WHERE launch.organization_id=@organization_id::uuid AND launch.project_id=ANY(@project_ids::uuid[]) AND launch.state='OPEN'
 AND (parent.state IN ('SUCCEEDED','FAILED','CANCELLED') OR child.state IN ('SUCCEEDED','FAILED','CANCELLED'))
ORDER BY launch.id LIMIT 129 FOR UPDATE OF launch;
