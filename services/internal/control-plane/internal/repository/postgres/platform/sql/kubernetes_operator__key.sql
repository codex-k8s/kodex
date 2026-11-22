-- name: kubernetes_operator__key :one
SELECT owner.cluster_uid::text,owner.node_uid::text,EXISTS(SELECT 1 FROM control_plane.kubernetes_retired_serving_keys retired WHERE retired.serving_key_sha256=owner.serving_key_sha256) FROM control_plane.kubernetes_serving_key_owners owner WHERE owner.serving_key_sha256=@serving_key_sha256;
