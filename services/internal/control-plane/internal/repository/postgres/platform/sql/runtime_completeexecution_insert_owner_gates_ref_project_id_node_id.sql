-- name: runtime_completeexecution_insert_owner_gates_ref_project_id_node_id :exec
INSERT INTO control_plane.owner_gates(ref,organization_id,project_id,root_run_id,node_id,title,prompt,context_summary,allowed_decisions,state) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'i18n:OWNER_GATE_REVIEW_TITLE','i18n:OWNER_GATE_REVIEW_PROMPT',$6,ARRAY['APPROVE','REJECT','REQUEST_CHANGES','CANCEL'],'OPEN')
