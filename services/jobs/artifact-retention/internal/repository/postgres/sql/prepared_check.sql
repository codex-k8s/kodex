-- name: prepared_check :one
SELECT to_regclass('control_plane.prepared_content') IS NOT NULL
 AND has_function_privilege(current_user,'control_plane.prepared_content_claim(text,integer,integer)','EXECUTE')
 AND has_function_privilege(current_user,'control_plane.prepared_content_finish(uuid,text,bigint,boolean,text,text)','EXECUTE')
 AND has_function_privilege(current_user,'control_plane.prepared_content_record_receipt(uuid,text,bigint,text,text,text,text,bigint)','EXECUTE')
 AND to_regprocedure('control_plane.kubernetes_file_origins_release(integer)') IS NOT NULL
 AND position('candidate.source_profile IN (''SYSTEM'',''PROJECT'')' IN pg_get_functiondef('control_plane.prepared_content_claim(text,integer,integer)'::regprocedure))>0
 AND NOT EXISTS(SELECT 1 FROM(VALUES
  ('control_plane.kubernetes_stage_cleanup_claim(text,integer,integer)'),
  ('control_plane.kubernetes_stage_cleanup_record(uuid,text,text,bigint,text,text,text,text,bigint)'),
  ('control_plane.kubernetes_stage_cleanup_complete(uuid,text,text,bigint,text,text,boolean)'),
  ('control_plane.kubernetes_file_source_cleanup_claim(text,integer,integer)'),
  ('control_plane.kubernetes_file_source_cleanup_record(uuid,text,bigint,text,text,text,text,bigint)'),
  ('control_plane.kubernetes_file_source_cleanup_complete(uuid,text,bigint,text,text,boolean)')
 ) required(signature) WHERE to_regprocedure(required.signature) IS NULL
  OR NOT has_function_privilege(session_user,required.signature,'EXECUTE'))
 AND position('KUBERNETES_FILE_SOURCE' IN pg_get_functiondef('control_plane.kubernetes_stage_cleanup_claim(text,integer,integer)'::regprocedure))>0
 AND position('KUBERNETES_FILE_SOURCE' IN pg_get_functiondef('control_plane.kubernetes_stage_cleanup_record(uuid,text,text,bigint,text,text,text,text,bigint)'::regprocedure))>0
 AND position('KUBERNETES_FILE_SOURCE' IN pg_get_functiondef('control_plane.kubernetes_stage_cleanup_complete(uuid,text,text,bigint,text,text,boolean)'::regprocedure))>0;
