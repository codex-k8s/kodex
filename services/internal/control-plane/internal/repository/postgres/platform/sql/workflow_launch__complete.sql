-- name: workflow_launch__complete :one
WITH completed AS (
 UPDATE control_plane.required_workflow_launches SET state=@state,completed_at=clock_timestamp() WHERE id=@launch_id::uuid AND state='OPEN' RETURNING proxy_node_id
), closed AS (
 UPDATE control_plane.run_nodes SET state=@state,finished_at=clock_timestamp(),version=version+1,next_actions=ARRAY['OPEN']
 WHERE id IN(SELECT proxy_node_id FROM completed) AND state NOT IN ('SUCCEEDED','FAILED','CANCELLED') RETURNING id
)
SELECT EXISTS(SELECT 1 FROM closed);
