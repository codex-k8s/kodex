BEGIN;
SET LOCAL ROLE control_plane_owner;
INSERT INTO control_plane.organizations(id, ref, name)
VALUES ('10000000-0000-4000-8000-000000000001','org_upgrade','Upgrade fixture');
INSERT INTO control_plane.subjects(id, organization_id, ref, issuer, external_subject_digest, display_name)
VALUES ('10000000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001',
    'sub_upgrade','https://example.test',repeat('a',64),'Upgrade actor');
INSERT INTO control_plane.projects(id, ref, organization_id, name, created_by)
VALUES ('10000000-0000-4000-8000-000000000003','prj_upgrade','10000000-0000-4000-8000-000000000001',
    'Upgrade project','10000000-0000-4000-8000-000000000002');
INSERT INTO control_plane.role_definitions(ref,organization_id,name,role_type)
VALUES ('rol_upgrade','10000000-0000-4000-8000-000000000001','Upgrade role','developer');
INSERT INTO control_plane.runtime_profiles(stable_key,name,provider,model,runtime_revision,resource_limits)
VALUES ('upgrade-fixture','Upgrade runtime','openai','synthetic','fixture','{}') ON CONFLICT DO NOTHING;
INSERT INTO control_plane.agents(ref, organization_id, project_id, role_definition_id, name, runtime_key, state, version)
SELECT 'agt_upgrade','10000000-0000-4000-8000-000000000001','10000000-0000-4000-8000-000000000003',
    (SELECT id FROM control_plane.role_definitions WHERE ref='rol_upgrade'), 'Upgrade agent',
    'upgrade-fixture', 'READY', 7;
INSERT INTO control_plane.schedules(id, ref, organization_id, project_id, name, target_type, target_ref,
    preset, cron_expression, timezone, input, session_policy, notification_policy, next_run_at, created_by, current_revision_id)
VALUES ('10000000-0000-4000-8000-000000000004','sch_upgrade','10000000-0000-4000-8000-000000000001',
    '10000000-0000-4000-8000-000000000003','Upgrade schedule','AGENT','agt_upgrade',
    'HOURLY','0 * * * *','UTC','{"task":"Existing automation"}','NEW_EACH_RUN','CONTROL_CENTER_ONLY',
    date_trunc('hour',clock_timestamp())+interval '1 hour','10000000-0000-4000-8000-000000000002',
    '10000000-0000-4000-8000-000000000005');
INSERT INTO control_plane.schedule_revisions(id,ref,organization_id,schedule_id,revision,name,target_type,target_ref,
    preset,cron_expression,timezone,input,session_policy,notification_policy,digest,created_by)
SELECT current_revision_id,'srev_upgrade01',organization_id,id,1,name,target_type,target_ref,
    preset,cron_expression,timezone,input,session_policy,notification_policy,repeat('a',64),created_by
FROM control_plane.schedules WHERE ref='sch_upgrade';
INSERT INTO control_plane.schedule_occurrences(ref,organization_id,schedule_id,scheduled_for,schedule_version,
    target_type,target_ref,run_name,input,input_digest,state,lease_ref,fence_digest,generation,workload_instance,
    lease_expires_at,schedule_revision_id)
SELECT 'occ_upgrade',organization_id,id,next_run_at-interval '1 hour',version,target_type,target_ref,name,input,
    encode(digest(convert_to(input::text,'UTF8'),'sha256'),'hex'),'CLAIMED','lea_upgrade',repeat('b',64),1,
    'old-worker',clock_timestamp()+interval '30 seconds',current_revision_id
FROM control_plane.schedules WHERE ref='sch_upgrade';
COMMIT;
