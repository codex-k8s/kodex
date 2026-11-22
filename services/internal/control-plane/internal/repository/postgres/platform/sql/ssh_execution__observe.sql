-- name: ssh_execution__observe :exec
INSERT INTO control_plane.ssh_execution_observations(intent_id,organization_id,outcome,result_body,result_digest,applied,terminal_state)
VALUES(@intent_id::uuid,@organization_id::uuid,@outcome,@result,@result_digest,@applied,@state);
