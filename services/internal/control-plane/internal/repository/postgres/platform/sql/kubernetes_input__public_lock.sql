-- name: kubernetes_input__public_lock :one
SELECT invocation.id::text FROM control_plane.integration_invocations invocation
WHERE invocation.id=@invocation_id::uuid AND invocation.organization_id=@organization_id::uuid FOR UPDATE;
