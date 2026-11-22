-- name: kubernetes_file_owner_ssh_binding_delete_denied :exec
DELETE FROM control_plane.ssh_invocation_identity_bindings WHERE false;
