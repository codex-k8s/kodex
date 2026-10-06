-- name: workflow_launch__existing :one
SELECT child.ref,launch.ref,edge.ref FROM control_plane.required_workflow_launches launch
JOIN control_plane.runs child ON child.id=launch.child_root_run_id
JOIN control_plane.run_edges edge ON edge.id=launch.callback_edge_id
WHERE launch.organization_id=@organization_id::uuid AND launch.origin_runtime_revision_id=@revision_id::uuid
 AND launch.workflow_ref=@workflow_ref AND launch.request_digest=@request_digest;
