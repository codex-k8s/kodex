-- name: commands_launchrun_insert_runs_ref_project_id_target_type :one
INSERT INTO control_plane.runs(
    ref, organization_id, project_id, session_id, workflow_version_id,
    target_type, target_ref, source, title, title_source, task, input, input_artifact_refs,
    state, initiated_by, concurrency_limit
) VALUES (
    @run_ref, @organization_id::uuid, @project_id::uuid, @session_id::uuid,
    NULLIF(@workflow_version_id, '')::uuid, @target_type, @target_ref, @source,
    @title, @title_source, @task, @input, @input_artifact_refs, 'QUEUED',
    @initiated_by::uuid, @concurrency_limit
)
RETURNING id::text
