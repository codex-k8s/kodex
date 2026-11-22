-- name: kubernetes_operator__insert :exec
INSERT INTO control_plane.kubernetes_operator_registry_revisions(revision,installation_ref,identity_namespace,payload,content_sha256) VALUES(@revision,@installation_ref,@identity_namespace,@payload,@content_sha256);
