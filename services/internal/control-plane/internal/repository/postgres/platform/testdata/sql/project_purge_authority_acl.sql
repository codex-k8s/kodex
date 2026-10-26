-- name: project_purge_authority_acl :many
SELECT name,has_table_privilege(session_user,name,'SELECT'),
       ARRAY(SELECT privilege FROM unnest(ARRAY['INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER']) privilege
             WHERE has_table_privilege(session_user,name,privilege)),
       current_user=session_user AND pg_has_role(session_user,'control_plane_runtime','MEMBER')
           AND NOT pg_has_role(session_user,'control_plane_owner','MEMBER'),
       control_plane.project_purge_authorized()
FROM unnest(ARRAY['control_plane.project_purge_context','control_plane.project_purge_targets']) name;
