-- name: configuration_addassistantturncommand_insert_session_turns_ref_session_id_actor_kind :exec
INSERT INTO control_plane.session_turns(ref,organization_id,session_id,turn_number,actor_kind,actor_ref,content,artifact_refs,state) VALUES($1,$2::uuid,$3::uuid,$4,'USER',$5,$6,$7,'COMPLETED')
