-- name: kubernetes_file_owner_ssh_orphan :exec
INSERT INTO control_plane.ssh_execution_observations(intent_id,organization_id,outcome,result_body,result_digest,applied,terminal_state)
VALUES($1::uuid,$2::uuid,'UNKNOWN','','',false,'UNKNOWN_OUTCOME');
