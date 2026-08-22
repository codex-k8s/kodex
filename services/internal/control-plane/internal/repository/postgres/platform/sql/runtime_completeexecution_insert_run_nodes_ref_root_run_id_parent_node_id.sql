-- name: runtime_completeexecution_insert_run_nodes_ref_root_run_id_parent_node_id :one
INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,parent_node_id,type,state,display_name,role,next_actions) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'HUMAN_GATE','WAITING','i18n:OWNER_GATE_NODE_NAME','i18n:OWNER_GATE_NODE_ROLE',ARRAY['OPEN','RESOLVE_GATE']) RETURNING id::text
