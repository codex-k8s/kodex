-- name: kubernetes_file_owner_ssh_constraints :one
SELECT count(*)=4 AND coalesce(bool_and(condeferrable AND NOT condeferred),false)
FROM pg_constraint WHERE
 (conrelid='control_plane.ssh_invocation_identity_bindings'::regclass AND conname IN('ssh_invocation_identity_bindings_invocation_id_fkey','ssh_invocation_identity_bindings_root_run_id_fkey'))
 OR(conrelid='control_plane.ssh_execution_intents'::regclass AND conname='ssh_execution_intents_invocation_id_fkey')
 OR(conrelid='control_plane.ssh_execution_observations'::regclass AND conname='ssh_execution_observations_intent_id_fkey');
