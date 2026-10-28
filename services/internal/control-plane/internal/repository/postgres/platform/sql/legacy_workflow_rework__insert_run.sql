-- name: legacy_workflow_rework__insert_run :one
INSERT INTO control_plane.runs(ref,organization_id,project_id,session_id,root_run_id,parent_run_id,
    workflow_version_id,retry_of_run_id,target_type,target_ref,source,title,task,input,state,attempt,initiated_by,started_at)
SELECT @run_ref,prior.organization_id,prior.project_id,@session_id::uuid,prior.root_run_id,@parent_run_id::uuid,
    prior.workflow_version_id,prior.id,'AGENT',prior.target_ref,'AGENT_DELEGATION',prior.title,@task,
    prior.input,'RUNNING',prior.attempt+1,prior.initiated_by,clock_timestamp()
FROM control_plane.runs prior WHERE prior.id=@prior_run_id::uuid AND prior.organization_id=@organization_id::uuid
    AND prior.state='SUCCEEDED' AND prior.attempt<2147483647
RETURNING id::text;
