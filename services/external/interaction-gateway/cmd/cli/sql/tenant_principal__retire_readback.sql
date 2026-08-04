SELECT principal.status,
       role.rolcanlogin,
       pg_has_role(principal.principal_name, 'interaction_gateway_runtime', 'member'),
       (SELECT count(*) FROM pg_catalog.pg_stat_activity activity
         WHERE activity.usename = principal.principal_name::text)
  FROM interaction_gateway_runtime_principals AS principal
  JOIN pg_catalog.pg_roles AS role ON role.rolname = principal.principal_name
 WHERE principal.generation = $1;
