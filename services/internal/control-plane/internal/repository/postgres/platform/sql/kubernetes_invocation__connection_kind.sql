-- name: kubernetes_invocation__connection_kind :one
SELECT EXISTS(SELECT 1 FROM control_plane.integration_connections
 WHERE organization_id=@organization_id::uuid AND ref=@connection_ref AND definition_key='kubernetes-managed');
