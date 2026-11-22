-- name: email_scope__invocation :one
SELECT COALESCE(approval_scope_id::text,'') FROM control_plane.integration_invocations
WHERE organization_id=@organization_id::uuid AND id=@invocation_id::uuid AND approval_policy='HUMAN_SCOPED';
