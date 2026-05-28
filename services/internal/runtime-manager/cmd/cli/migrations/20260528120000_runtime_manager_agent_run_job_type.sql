-- +goose Up
ALTER TABLE runtime_manager_jobs
    DROP CONSTRAINT runtime_manager_jobs_type_chk;

ALTER TABLE runtime_manager_jobs
    ADD CONSTRAINT runtime_manager_jobs_type_chk
        CHECK (job_type IN ('mirror', 'build', 'deploy', 'cleanup', 'health_check', 'housekeeping', 'workspace_materialization', 'agent_run'));

-- +goose Down
ALTER TABLE runtime_manager_jobs
    DROP CONSTRAINT runtime_manager_jobs_type_chk;

ALTER TABLE runtime_manager_jobs
    ADD CONSTRAINT runtime_manager_jobs_type_chk
        CHECK (job_type IN ('mirror', 'build', 'deploy', 'cleanup', 'health_check', 'housekeeping', 'workspace_materialization'));
