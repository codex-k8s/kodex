-- name: kubernetes_file__readiness :one
SELECT to_regclass('control_plane.kubernetes_file_input_origins') IS NOT NULL
 AND to_regclass('control_plane.kubernetes_file_input_sources') IS NOT NULL
 AND to_regclass('control_plane.kubernetes_file_input_frames') IS NOT NULL
 AND to_regclass('control_plane.kubernetes_file_delete_admissions') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_attribute attribute WHERE attribute.attrelid=to_regclass('control_plane.kubernetes_input_stages')
  AND attribute.attname='object_key' AND attribute.attnum>0 AND NOT attribute.attisdropped AND NOT attribute.attnotnull)
 AND EXISTS(SELECT 1 FROM pg_constraint bound WHERE bound.conrelid=to_regclass('control_plane.kubernetes_input_stages')
  AND bound.conname='kubernetes_input_file_metadata_no_object' AND bound.contype='c' AND bound.convalidated)
 AND to_regprocedure('control_plane.runtime_file_source_visible_revision(uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_file_origins_release(integer)') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_file_source_cleanup_eligible(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('control_plane.repair_kubernetes_file_source_cleanup(uuid,uuid,bigint)') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_file_source_cleanup_claim(text,integer,integer)') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_file_source_cleanup_record(uuid,text,bigint,text,text,text,text,bigint)') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_file_source_cleanup_complete(uuid,text,bigint,text,text,boolean)') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('control_plane.kubernetes_file_input_sources')
  AND tgname='kubernetes_file_cleanup_transition' AND tgenabled='O')
 AND to_regprocedure('control_plane.kubernetes_stage_cleanup_claim(text,integer,integer)') IS NOT NULL
 AND position('KUBERNETES_FILE_SOURCE' IN pg_get_functiondef('control_plane.kubernetes_stage_cleanup_claim(text,integer,integer)'::regprocedure))>0
 AND position('KUBERNETES_FILE_SOURCE' IN pg_get_functiondef('control_plane.kubernetes_stage_cleanup_record(uuid,text,text,bigint,text,text,text,text,bigint)'::regprocedure))>0
 AND position('KUBERNETES_FILE_SOURCE' IN pg_get_functiondef('control_plane.kubernetes_stage_cleanup_complete(uuid,text,text,bigint,text,text,boolean)'::regprocedure))>0
 AND NOT EXISTS(SELECT 1 FROM(VALUES('kubernetes_file_origin_guard','control_plane.kubernetes_file_input_origins'),('kubernetes_file_source_guard','control_plane.kubernetes_file_input_sources'),('kubernetes_file_source_adoption','control_plane.kubernetes_file_input_sources'),('kubernetes_file_frame_guard','control_plane.kubernetes_file_input_frames'),('kubernetes_file_frame_binding','control_plane.kubernetes_input_stages'),('kubernetes_file_ack','control_plane.kubernetes_input_stages'),('kubernetes_file_prepared_guard','control_plane.prepared_content'),('kubernetes_file_stage_cleanup_guard','control_plane.kubernetes_input_stages'),('kubernetes_file_delete_admission_guard','control_plane.kubernetes_file_delete_admissions')) required(trigger_name,relation_name)
  WHERE NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass(required.relation_name) AND tgname=required.trigger_name AND tgenabled='O'))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('control_plane.kubernetes_file_input_origins','SELECT'),('control_plane.kubernetes_file_input_origins','INSERT'),('control_plane.kubernetes_file_input_sources','SELECT'),('control_plane.kubernetes_file_input_sources','INSERT'),('control_plane.kubernetes_file_input_sources','UPDATE'),('control_plane.kubernetes_file_input_frames','SELECT'),('control_plane.kubernetes_file_input_frames','INSERT')) required(relation_name,privileges)
  WHERE NOT has_table_privilege(session_user,required.relation_name,required.privileges))
 AND position('candidate.source_profile IN (''SYSTEM'',''PROJECT'')' IN pg_get_functiondef('control_plane.prepared_content_claim(text,integer,integer)'::regprocedure))>0
 AND position('kubernetes_file_origin_retained' IN pg_get_functiondef('control_plane.artifact_has_retained_revisions(uuid)'::regprocedure))>0;
