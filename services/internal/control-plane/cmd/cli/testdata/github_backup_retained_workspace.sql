-- Только disposable fixture. Существующий native seed задаёт собственные
-- organization/subject/provider/project/session; writer/bytes не создаются.
INSERT INTO control_plane.runtime_profiles(stable_key,name,provider,model,runtime_revision,resource_limits,enabled)
VALUES('synthetic-retained-backup','Synthetic retained backup','synthetic','synthetic','fixture','{}',false);
INSERT INTO control_plane.role_definitions(id,ref,organization_id,project_id,name,role_type,created_by)
VALUES('00000000-0000-4000-8000-000000000005','role_retained_backup','00000000-0000-4000-8000-000000000001',
 '00000000-0000-4000-8000-000000000004','Synthetic retained role','developer','00000000-0000-4000-8000-000000000002');
INSERT INTO control_plane.agents(id,ref,organization_id,project_id,role_definition_id,name,runtime_key,state,enabled,created_by)
VALUES('00000000-0000-4000-8000-000000000006','agt_synthetic_cutover','00000000-0000-4000-8000-000000000001',
 '00000000-0000-4000-8000-000000000004','00000000-0000-4000-8000-000000000005','Synthetic retained agent',
 'synthetic-retained-backup','DRAFT',false,'00000000-0000-4000-8000-000000000002');
INSERT INTO control_plane.session_workspace_storage(ref,organization_id,project_id,session_id,agent_id,state)
SELECT 'wks_retained_empty',organization_id,project_id,id,'00000000-0000-4000-8000-000000000006','EMPTY'
FROM control_plane.sessions WHERE ref='ses_native_cutover';
INSERT INTO control_plane.sessions(ref,organization_id,project_id,target_type,target_ref,provider_account_id,state,created_by)
SELECT 'ses_retained_blocked',organization_id,project_id,target_type,target_ref,provider_account_id,state,created_by
FROM control_plane.sessions WHERE ref='ses_native_cutover';
INSERT INTO control_plane.session_workspace_storage(ref,organization_id,project_id,session_id,agent_id,state)
SELECT 'wks_retained_blocked',organization_id,project_id,id,'00000000-0000-4000-8000-000000000006','EMPTY'
FROM control_plane.sessions WHERE ref='ses_retained_blocked';
UPDATE control_plane.session_workspace_storage
SET state='BLOCKED',block_reason='POD_MISSING',version=version+1,updated_at=clock_timestamp()
WHERE ref='wks_retained_blocked';
