BEGIN;
SET LOCAL ROLE control_plane_owner;
INSERT INTO control_plane.organizations(id,ref,name) VALUES ('79000000-0000-4000-8000-000000000001','org_artifact_upgrade','Synthetic artifact upgrade');
INSERT INTO control_plane.subjects(id,organization_id,ref,issuer,external_subject_digest,display_name)
 VALUES ('79000000-0000-4000-8000-000000000002','79000000-0000-4000-8000-000000000001','sub_artifact_upgrade','https://fixture.invalid',repeat('a',64),'Synthetic actor');
INSERT INTO control_plane.projects(id,organization_id,ref,name,created_by)
 VALUES ('79000000-0000-4000-8000-000000000003','79000000-0000-4000-8000-000000000001','prj_artifact_upgrade','Synthetic project','79000000-0000-4000-8000-000000000002');
INSERT INTO control_plane.artifacts(id,ref,organization_id,project_id,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,revision,version,created_by)
SELECT ('79000000-0000-4000-8000-00000000000'||position)::uuid,'art_upgrade_'||position,'79000000-0000-4000-8000-000000000001','79000000-0000-4000-8000-000000000003',
 'same-name.txt','text/plain',4,'sha256:'||repeat(position::text,64),'CONTROL_CENTER','CLEAN','obj_upgrade_'||position,'AVAILABLE',revision,version,'79000000-0000-4000-8000-000000000002'
FROM (VALUES(4,7,12),(5,8,13)) fixture(position,revision,version);
INSERT INTO control_plane.artifact_content(artifact_id,object_key,object_version,object_etag,digest,size_bytes)
SELECT id,'disposable/upgrade/'||ref,'exact-version-'||revision,'fixture',digest,size_bytes FROM control_plane.artifacts WHERE ref LIKE 'art_upgrade_%';
INSERT INTO control_plane.artifact_bindings(artifact_id,target_kind,target_ref,created_by)
SELECT id,'KNOWLEDGE','agt_synthetic_upgrade','79000000-0000-4000-8000-000000000002' FROM control_plane.artifacts WHERE ref='art_upgrade_4';
INSERT INTO control_plane.artifact_download_grants(ref,organization_id,project_id,artifact_id,artifact_version,subject_id,purpose,expires_at)
SELECT 'adg_upgrade_'||revision,organization_id,project_id,id,version,created_by,'DOWNLOAD',clock_timestamp()+interval '1 minute' FROM control_plane.artifacts WHERE ref LIKE 'art_upgrade_%';
CREATE TABLE public.artifact_upgrade_expected(kind text,ref text,snapshot jsonb,PRIMARY KEY(kind,ref));
INSERT INTO public.artifact_upgrade_expected SELECT 'artifact',ref,to_jsonb(artifact) FROM control_plane.artifacts artifact WHERE ref LIKE 'art_upgrade_%';
INSERT INTO public.artifact_upgrade_expected SELECT 'content',artifact.ref,to_jsonb(content) FROM control_plane.artifact_content content JOIN control_plane.artifacts artifact ON artifact.id=content.artifact_id WHERE artifact.ref LIKE 'art_upgrade_%';
INSERT INTO public.artifact_upgrade_expected SELECT 'binding',artifact.ref,to_jsonb(binding) FROM control_plane.artifact_bindings binding JOIN control_plane.artifacts artifact ON artifact.id=binding.artifact_id WHERE artifact.ref LIKE 'art_upgrade_%';
INSERT INTO public.artifact_upgrade_expected SELECT 'grant',grant_row.ref,to_jsonb(grant_row) FROM control_plane.artifact_download_grants grant_row WHERE grant_row.ref LIKE 'adg_upgrade_%';
INSERT INTO public.artifact_upgrade_expected VALUES ('oid','head',to_jsonb('control_plane.artifacts'::regclass::oid));
COMMIT;
