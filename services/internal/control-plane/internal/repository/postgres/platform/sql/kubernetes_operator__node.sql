-- name: kubernetes_operator__node :one
SELECT pin.generation,pin.content_sha256,EXISTS(SELECT 1 FROM control_plane.kubernetes_retired_node_pins retired WHERE retired.ref=pin.ref) FROM control_plane.kubernetes_node_transport_pins pin WHERE pin.ref=@ref;
