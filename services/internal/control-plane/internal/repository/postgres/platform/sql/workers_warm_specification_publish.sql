-- name: workers_warm_specification_publish :one
UPDATE control_plane.assistant_runtime
SET desired_spec_version = $2,
    desired_spec_digest = $3,
    desired_spec_ref = $4,
    desired_runtime_revision = $4,
    runtime_state = CASE WHEN $5 THEN 'RECOVERING' ELSE runtime_state END,
    version = version + CASE WHEN $5 THEN 1 ELSE 0 END
WHERE organization_id = $1::uuid
RETURNING version, runtime_state
