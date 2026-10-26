-- name: legacy_workflow_rework__insert_retry :exec
INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label)
VALUES(@edge_ref,@organization_id::uuid,@root_run_id::uuid,@prior_node_id::uuid,@node_id::uuid,'RETRY_OF','');
