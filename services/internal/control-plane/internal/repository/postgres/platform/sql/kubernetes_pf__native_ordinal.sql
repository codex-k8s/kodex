-- name: kubernetes_pf__native_ordinal :one
SELECT EXISTS(SELECT 1 FROM control_plane.kubernetes_port_forward_frames
WHERE organization_id=@organization_id::uuid AND stream_id=@stream_id::uuid AND connection_id=@connection_id::uuid AND direction='OUTPUT' AND native_ordinal=@native_ordinal);
