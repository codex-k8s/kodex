-- name: kubernetes_file_owner_ssh_defer :exec
SET CONSTRAINTS control_plane.ssh_execution_observations_intent_id_fkey DEFERRED;
