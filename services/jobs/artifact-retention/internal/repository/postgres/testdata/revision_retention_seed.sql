BEGIN;
SET LOCAL ROLE control_plane_owner;
INSERT INTO control_plane.organizations(id,ref,name) VALUES ('78000000-0000-4000-8000-000000000001','org_retention_fixture','Synthetic retention');
INSERT INTO control_plane.subjects(id,organization_id,ref,issuer,external_subject_digest,display_name)
 VALUES ('78000000-0000-4000-8000-000000000002','78000000-0000-4000-8000-000000000001','sub_retention_fixture','https://fixture.invalid',repeat('a',64),'Synthetic actor');
INSERT INTO control_plane.projects(id,organization_id,ref,name,created_by)
 VALUES ('78000000-0000-4000-8000-000000000003','78000000-0000-4000-8000-000000000001','prj_retention_fixture','Synthetic project','78000000-0000-4000-8000-000000000002');
INSERT INTO control_plane.artifacts(id,ref,organization_id,project_id,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,revision,created_by)
 VALUES ('78000000-0000-4000-8000-000000000004','art_retention_fixture','78000000-0000-4000-8000-000000000001','78000000-0000-4000-8000-000000000003','old.txt','text/plain',3,$3,'CONTROL_CENTER','CLEAN','obj_retention_fixture','AVAILABLE',7,'78000000-0000-4000-8000-000000000002');
INSERT INTO control_plane.artifact_content(artifact_id,object_key,object_version,object_etag,digest,size_bytes)
 VALUES ('78000000-0000-4000-8000-000000000004',$1,$2,'fixture',$3,3);
INSERT INTO control_plane.artifact_revisions(id,ref,artifact_id,revision,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,created_by,created_at)
 VALUES ('78000000-0000-4000-8000-000000000005','arv_retention_fixture','78000000-0000-4000-8000-000000000004',8,'new.txt','text/plain',3,$6,'CONTROL_CENTER','CLEAN','obj_retention_next','AVAILABLE','78000000-0000-4000-8000-000000000002',clock_timestamp());
INSERT INTO control_plane.artifact_revision_content(revision_id,object_key,object_version,object_etag,digest,size_bytes)
 VALUES ('78000000-0000-4000-8000-000000000005',$4,$5,'fixture',$6,3);
INSERT INTO control_plane.artifact_bindings(artifact_id,target_kind,target_ref,created_by)
 VALUES ('78000000-0000-4000-8000-000000000004','KNOWLEDGE','agt_retention_fixture','78000000-0000-4000-8000-000000000002');
INSERT INTO control_plane.artifact_download_grants(ref,organization_id,project_id,artifact_id,artifact_version,subject_id,purpose,expires_at)
 VALUES ('adg_retention_fixture','78000000-0000-4000-8000-000000000001','78000000-0000-4000-8000-000000000003','78000000-0000-4000-8000-000000000004',1,'78000000-0000-4000-8000-000000000002','DOWNLOAD',clock_timestamp()+interval '1 minute');
UPDATE control_plane.artifact_heads SET current_revision_id='78000000-0000-4000-8000-000000000005',version=version+1 WHERE ref='art_retention_fixture';
UPDATE control_plane.artifact_heads SET lifecycle_state='DELETED',deleted_at=clock_timestamp()-interval '31 days',purge_after=clock_timestamp()-interval '1 day',version=version+1 WHERE ref='art_retention_fixture';
COMMIT;
