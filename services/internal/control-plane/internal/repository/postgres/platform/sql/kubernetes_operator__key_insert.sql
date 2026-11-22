-- name: kubernetes_operator__key_insert :exec
INSERT INTO control_plane.kubernetes_serving_key_owners(first_target_ref,first_target_revision,serving_key_sha256,cluster_uid,node_uid,registry_revision) VALUES(@target_ref,@target_revision,@serving_key_sha256,@cluster_uid::uuid,@node_uid::uuid,@registry_revision) ON CONFLICT(serving_key_sha256) DO NOTHING;
