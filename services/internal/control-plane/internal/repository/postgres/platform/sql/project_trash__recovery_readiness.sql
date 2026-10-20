SELECT to_regprocedure('control_plane.project_purge_recovery_snapshot(uuid,text,text)') IS NOT NULL
 AND to_regprocedure('control_plane.project_purge_recovery_resolve(uuid,text,bigint,text,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('control_plane.project_purge_recovery_claim(text,integer,text)') IS NOT NULL
 AND to_regprocedure('control_plane.project_purge_recovery_pin(uuid,text,bigint,text,text,text)') IS NOT NULL
 AND to_regprocedure('control_plane.project_purge_recovery_finish(uuid,text,bigint,text,text)') IS NOT NULL;
