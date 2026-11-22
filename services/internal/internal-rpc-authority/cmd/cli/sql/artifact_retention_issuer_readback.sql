-- name: artifact_retention_issuer_readback :one
SELECT current_database()='internal_rpc_authority' AND EXISTS(
 SELECT 1
 FROM pg_catalog.pg_roles AS login
 JOIN pg_catalog.pg_auth_members AS membership ON membership.member=login.oid
 JOIN pg_catalog.pg_roles AS capability ON capability.oid=membership.roleid
 JOIN internal_rpc_authority.authority_workload_database_identities AS ident ON ident.principal=login.rolname
 WHERE login.rolname='ira_artifact_retention_issuer_g1' AND login.rolcanlogin AND NOT login.rolsuper AND NOT login.rolinherit
   AND NOT login.rolcreatedb AND NOT login.rolcreaterole AND NOT login.rolreplication AND NOT login.rolbypassrls
   AND (SELECT count(*) FROM pg_catalog.pg_auth_members member WHERE member.member=login.oid)=1
   AND capability.rolname='internal_rpc_authority_issuer' AND membership.set_option
   AND NOT membership.admin_option AND NOT membership.inherit_option
   AND ident.workload_id='artifact-retention' AND ident.capability='ISSUER' AND ident.generation=1 AND ident.lifecycle_status='CURRENT'
   AND pg_catalog.has_database_privilege(login.oid,current_database(),'CONNECT')
);
