-- name: runtime_delegateexecution_materialize_planned_node :one
UPDATE control_plane.run_nodes
SET run_id=@run_id::uuid,turn_id=@turn_id::uuid,state='QUEUED',materialization_state='MATERIALIZED',
    input_summary=@input_summary,next_actions=ARRAY['OPEN','CANCEL'],version=version+1
WHERE id=@node_id::uuid AND state='PLANNED' AND materialization_state='PLANNED'
RETURNING ref
