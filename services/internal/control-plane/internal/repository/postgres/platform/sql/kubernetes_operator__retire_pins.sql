-- name: kubernetes_operator__retire_pins :exec
INSERT INTO control_plane.kubernetes_retired_node_pins(ref,registry_revision) SELECT ref,@registry_revision FROM control_plane.kubernetes_node_transport_pins WHERE NOT(ref=ANY(@active_refs::text[])) ON CONFLICT(ref) DO NOTHING;
