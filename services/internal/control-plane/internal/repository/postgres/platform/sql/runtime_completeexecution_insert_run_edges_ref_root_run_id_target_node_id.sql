-- name: platform__runtime_completeexecution_insert_run_edges_ref_root_run_id_target_node_id :exec
INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'WAITING_FOR','')
