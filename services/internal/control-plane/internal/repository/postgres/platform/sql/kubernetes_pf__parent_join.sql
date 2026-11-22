-- name: kubernetes_pf__parent_join :exec
UPDATE control_plane.kubernetes_port_forward_connections SET native_joined_at=coalesce(native_joined_at,clock_timestamp())
WHERE organization_id=@organization_id::uuid AND stream_id=@stream_id::uuid AND closed_at IS NOT NULL;
