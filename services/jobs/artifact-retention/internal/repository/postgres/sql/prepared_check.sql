-- name: prepared_check :one
SELECT to_regclass('control_plane.prepared_content') IS NOT NULL
 AND has_function_privilege(current_user,'control_plane.prepared_content_claim(text,integer,integer)','EXECUTE')
 AND has_function_privilege(current_user,'control_plane.prepared_content_finish(uuid,text,bigint,boolean,text,text)','EXECUTE')
 AND has_function_privilege(current_user,'control_plane.prepared_content_record_receipt(uuid,text,bigint,text,text,text,text,bigint)','EXECUTE');
