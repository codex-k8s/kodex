SELECT p.prosrc,
       pg_get_userbyid(p.proowner)='control_plane_owner'
       AND NOT p.prosecdef AND p.provolatile='v'
       AND p.proconfig=ARRAY['search_path=pg_catalog, control_plane']
       AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
       AND NOT EXISTS (
         SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
         WHERE acl.grantee NOT IN ('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
            OR acl.grantee='control_plane_runtime'::regrole
               AND (acl.is_grantable OR acl.privilege_type<>'EXECUTE')) AS exact_boundary
FROM pg_proc p
WHERE p.oid='control_plane.guard_workflow_effect_records()'::regprocedure;
