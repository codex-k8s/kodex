-- name: platform__runtime_delegateexecution_10 :exec
INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'CALLBACK_TO','Вернуть результат родителю')
