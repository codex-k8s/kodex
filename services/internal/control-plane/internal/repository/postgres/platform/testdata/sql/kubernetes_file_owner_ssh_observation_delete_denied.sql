-- name: kubernetes_file_owner_ssh_observation_delete_denied :exec
DELETE FROM control_plane.ssh_execution_observations WHERE false;
