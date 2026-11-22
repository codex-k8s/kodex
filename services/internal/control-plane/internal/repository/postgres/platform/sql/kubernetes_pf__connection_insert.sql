-- name: kubernetes_pf__connection_insert :one
INSERT INTO control_plane.kubernetes_port_forward_connections(ref,organization_id,stream_id,actor_id,generation,slot,
 open_key,open_intent_digest,open_expected_version,open_stage_id,state)
VALUES(@ref,@organization_id::uuid,@stream_id::uuid,@actor_id::uuid,@generation,@slot,@open_key,@open_intent_digest,@expected_version,@stage_id::uuid,'PREPARED')
RETURNING id::text;
