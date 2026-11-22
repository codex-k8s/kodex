-- name: kubernetes_setup__begin_receipt :one
SELECT setup.ref,connection.ref,setup.intent_sha256
FROM control_plane.kubernetes_identity_setups setup
JOIN control_plane.integration_connections connection ON connection.id=setup.connection_id
 AND connection.organization_id=setup.organization_id
WHERE setup.organization_id=@organization_id::uuid AND setup.actor_id=@actor_id::uuid
 AND setup.begin_idempotency_key=@begin_key;
