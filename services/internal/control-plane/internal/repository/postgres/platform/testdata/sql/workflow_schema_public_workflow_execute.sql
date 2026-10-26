-- name: workflow_schema_public_workflow_execute :exec
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_effect_records() TO PUBLIC;
