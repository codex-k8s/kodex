-- name: repository_bundle__invocation_operation :one
SELECT operation FROM control_plane.integration_invocations
WHERE organization_id=@organization_id::uuid AND ref=@invocation_ref;
