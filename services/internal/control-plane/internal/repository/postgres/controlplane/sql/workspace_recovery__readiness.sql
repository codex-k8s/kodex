SELECT to_regprocedure('control_plane.next_workspace_recovery_candidate()') IS NOT NULL
   AND to_regprocedure(
       'control_plane.switch_runtime_workspace_context(uuid,uuid,uuid,name,bigint,text,uuid,bigint,bytea)'
   ) IS NOT NULL;
