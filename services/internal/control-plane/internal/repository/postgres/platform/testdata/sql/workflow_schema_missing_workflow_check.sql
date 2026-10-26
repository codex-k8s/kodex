-- name: workflow_schema_missing_workflow_check :exec
ALTER TABLE control_plane.run_nodes DROP CONSTRAINT workflow_journal_bounds;
