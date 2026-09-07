-- name: workers_warm_specification_lock :one
SELECT desired_spec_version, desired_spec_digest, desired_spec_ref
FROM control_plane.assistant_runtime
WHERE organization_id = $1::uuid
FOR UPDATE
