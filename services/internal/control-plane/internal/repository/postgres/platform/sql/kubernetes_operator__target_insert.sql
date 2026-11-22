-- name: kubernetes_operator__target_insert :exec
INSERT INTO control_plane.kubernetes_target_revisions(ref,revision,registry_revision,cluster_uid,payload,content_sha256) VALUES(@ref,@revision,@registry_revision,@cluster_uid::uuid,@payload,@content_sha256) ON CONFLICT(ref,revision) DO NOTHING;
