-- name: workflow_schema_disabled_workflow_guard :exec
ALTER TABLE control_plane.run_nodes DISABLE TRIGGER workflow_effect_records_guard;
