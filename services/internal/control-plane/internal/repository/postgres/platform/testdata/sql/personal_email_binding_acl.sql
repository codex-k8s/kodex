WITH required_relation(relation_name) AS (
 VALUES ('email_invocation_mailbox_bindings'),('email_approval_scope_bindings')
), runtime_role AS (
 SELECT oid FROM pg_catalog.pg_roles WHERE rolname='control_plane_runtime'
)
SELECT COALESCE(count(*)=2 AND bool_and(
 has_table_privilege(runtime_role.oid,relation.oid,'SELECT')
 AND has_table_privilege(runtime_role.oid,relation.oid,'INSERT')
 AND NOT has_table_privilege(runtime_role.oid,relation.oid,'SELECT WITH GRANT OPTION')
 AND NOT has_table_privilege(runtime_role.oid,relation.oid,'INSERT WITH GRANT OPTION')
 AND NOT has_any_column_privilege(runtime_role.oid,relation.oid,'UPDATE')
 AND NOT EXISTS(
  SELECT 1 FROM unnest(ARRAY['UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER','MAINTAIN']) forbidden(privilege_name)
  WHERE has_table_privilege(runtime_role.oid,relation.oid,forbidden.privilege_name)
 )
 AND NOT EXISTS(
  SELECT 1 FROM pg_catalog.aclexplode(COALESCE(relation.relacl,pg_catalog.acldefault('r',relation.relowner))) acl
  WHERE acl.grantee=0 OR (acl.grantee=runtime_role.oid
   AND (acl.privilege_type NOT IN('SELECT','INSERT') OR acl.is_grantable))
 )
 AND NOT EXISTS(
  SELECT 1 FROM pg_catalog.pg_attribute attribute
  CROSS JOIN LATERAL pg_catalog.aclexplode(attribute.attacl) acl
  WHERE attribute.attrelid=relation.oid AND attribute.attnum>0 AND NOT attribute.attisdropped
   AND (acl.grantee=0 OR (acl.grantee=runtime_role.oid
    AND (acl.privilege_type NOT IN('SELECT','INSERT') OR acl.is_grantable)))
 )
),false)
FROM required_relation
JOIN pg_catalog.pg_namespace namespace ON namespace.nspname='control_plane'
JOIN pg_catalog.pg_class relation ON relation.relnamespace=namespace.oid
 AND relation.relname=required_relation.relation_name AND relation.relkind='r'
CROSS JOIN runtime_role;
