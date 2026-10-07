-- name: workflow_launch__bound :one
WITH RECURSIVE ancestors AS (
 SELECT @root_run_id::uuid AS id,0 AS depth
 UNION ALL SELECT launch.origin_root_run_id,ancestor.depth+1
 FROM ancestors ancestor JOIN control_plane.required_workflow_launches launch ON launch.child_root_run_id=ancestor.id
 WHERE launch.organization_id=@organization_id::uuid AND ancestor.depth<9
), tree AS (
 SELECT id FROM ancestors ORDER BY depth DESC LIMIT 1
), descendants AS (
 SELECT id FROM tree
 UNION ALL SELECT launch.child_root_run_id FROM descendants parent
 JOIN control_plane.required_workflow_launches launch ON launch.origin_root_run_id=parent.id
 WHERE launch.organization_id=@organization_id::uuid
)
SELECT (SELECT count(*) FROM control_plane.required_workflow_launches WHERE organization_id=@organization_id::uuid AND origin_root_run_id=@root_run_id::uuid)<16
 AND (SELECT max(depth) FROM ancestors)<8
 AND (SELECT count(*) FROM descendants)<128;
