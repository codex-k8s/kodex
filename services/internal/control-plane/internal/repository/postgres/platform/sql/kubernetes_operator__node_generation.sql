-- name: kubernetes_operator__node_generation :one
SELECT coalesce(max(generation),0) FROM control_plane.kubernetes_node_transport_pins WHERE cluster_uid=@cluster_uid::uuid AND node_uid=@node_uid::uuid AND protocol=@protocol;
