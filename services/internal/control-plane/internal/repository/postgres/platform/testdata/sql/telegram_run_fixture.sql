-- name: telegram_run_fixture :one
-- Только синтетическая строка terminal Run в отдельной disposable БД; provider effect не выполняется.
WITH identity AS (SELECT gen_random_uuid() AS id)
INSERT INTO control_plane.runs(id,ref,organization_id,project_id,root_run_id,target_type,target_ref,source,title,task,state,initiated_by,finished_at)
SELECT identity.id,@run_ref,project.organization_id,project.id,identity.id,'AGENT',agent.ref,'CONTROL_CENTER',
 'PRIVATE_TITLE_MUST_NOT_LEAVE','PRIVATE_PROMPT_MUST_NOT_LEAVE','SUCCEEDED',@actor_id::uuid,clock_timestamp()
FROM identity,control_plane.projects project
JOIN control_plane.agents agent ON agent.project_id=project.id AND agent.organization_id=project.organization_id AND agent.ref=@agent_ref
WHERE project.organization_id=@organization_id::uuid AND project.ref=@project_ref
RETURNING id::text,project_id::text;
