INSERT INTO control_plane.organizations(id,ref,name)
VALUES('00000000-0000-4000-8000-000000000001','org_native_cutover','Synthetic native cutover');
INSERT INTO control_plane.subjects(id,organization_id,ref,issuer,external_subject_digest,display_name)
VALUES('00000000-0000-4000-8000-000000000002','00000000-0000-4000-8000-000000000001',
 'usr_native_cutover','https://identity.example.invalid',repeat('a',64),'Synthetic owner');
INSERT INTO control_plane.provider_definitions(stable_key,name,adapter_key,enabled)
VALUES('synthetic-native-cutover','Synthetic provider','synthetic-native-cutover',false);
INSERT INTO control_plane.provider_accounts(id,ref,organization_id,definition_key,stable_key,name,state,enabled,created_by)
VALUES('00000000-0000-4000-8000-000000000003','pacc_native_cutover','00000000-0000-4000-8000-000000000001',
 'synthetic-native-cutover','synthetic-native-cutover','Synthetic account','PENDING_AUTHORIZATION',false,
 '00000000-0000-4000-8000-000000000002');
INSERT INTO control_plane.projects(id,ref,organization_id,name,created_by)
VALUES('00000000-0000-4000-8000-000000000004','prj_native_cutover','00000000-0000-4000-8000-000000000001',
 'Synthetic project','00000000-0000-4000-8000-000000000002');
INSERT INTO control_plane.sessions(ref,organization_id,project_id,target_type,target_ref,provider_account_id,state,created_by)
VALUES('ses_native_cutover','00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000004',
 'AGENT','agt_synthetic_cutover','00000000-0000-4000-8000-000000000003','CLOSED','00000000-0000-4000-8000-000000000002');
