-- name: workflow_journal_mutate :exec
UPDATE control_plane.run_nodes
SET workflow_attempts=@attempts::jsonb,workflow_inputs=@inputs::jsonb
WHERE ref=@node_ref;
