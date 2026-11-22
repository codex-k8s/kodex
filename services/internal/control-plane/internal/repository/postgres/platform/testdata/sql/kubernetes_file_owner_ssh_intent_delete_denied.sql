-- name: kubernetes_file_owner_ssh_intent_delete_denied :exec
DELETE FROM control_plane.ssh_execution_intents WHERE false;
