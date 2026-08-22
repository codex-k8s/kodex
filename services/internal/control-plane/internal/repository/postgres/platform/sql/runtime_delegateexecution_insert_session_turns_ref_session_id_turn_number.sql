-- name: platform__runtime_delegateexecution_insert_session_turns_ref_session_id_turn_number :one
INSERT INTO control_plane.session_turns(ref,organization_id,session_id,run_id,turn_number,actor_kind,actor_ref,content,state) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5,'AGENT',$6,$7,'QUEUED') RETURNING id::text
