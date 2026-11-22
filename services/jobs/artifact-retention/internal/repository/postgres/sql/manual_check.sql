-- name: manual_check :one
SELECT to_regprocedure('control_plane.artifact_manual_retention_claim(text,integer,integer)') IS NOT NULL
 AND to_regprocedure('control_plane.artifact_manual_retention_complete(uuid,text,bigint)') IS NOT NULL
 AND has_function_privilege(session_user,'control_plane.artifact_manual_retention_claim(text,integer,integer)','EXECUTE')
 AND has_function_privilege(session_user,'control_plane.artifact_manual_retention_complete(uuid,text,bigint)','EXECUTE')
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_constraint WHERE conrelid='control_plane.artifact_heads'::regclass
  AND conname='artifact_manual_purge_receipt_fk' AND contype='f' AND convalidated);
