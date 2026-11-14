-- Закрытая identity sequence канонического bootstrap. Public foreign sequences
-- не получают backup grants при GRANT ON ALL SEQUENCES. Table-only и canonical
-- допускают только отсутствие backup ACL либо точные SELECT+USAGE.
WITH registered AS (
 SELECT c.oid,c.relowner,c.relacl
 FROM pg_catalog.pg_class c
 JOIN pg_catalog.pg_sequence s ON s.seqrelid=c.oid
 WHERE c.relnamespace='public'::regnamespace AND c.relname='goose_db_version_id_seq'
 AND c.relkind='S' AND c.relowner='control_plane_owner'::regrole
 AND c.relpersistence='p' AND c.reloptions IS NULL
 AND s.seqtypid='integer'::regtype AND s.seqstart=1 AND s.seqincrement=1
 AND s.seqmin=1 AND s.seqmax=2147483647 AND s.seqcache=1 AND NOT s.seqcycle
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_depend d JOIN pg_catalog.pg_attribute a
  ON a.attrelid=d.refobjid AND a.attnum=d.refobjsubid
  WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.objsubid=0
   AND d.refclassid='pg_class'::regclass AND d.refobjid='public.goose_db_version'::regclass
   AND d.deptype='i' AND a.attname='id' AND a.atttypid='integer'::regtype AND a.attidentity='d' AND NOT a.attisdropped)
), acl AS (
 SELECT a.* FROM registered r CROSS JOIN LATERAL pg_catalog.aclexplode(coalesce(r.relacl,pg_catalog.acldefault('S',r.relowner))) a
)
SELECT (SELECT count(*) FROM pg_catalog.pg_class WHERE relnamespace='public'::regnamespace AND relkind='S')=1
 AND (SELECT count(*) FROM registered)=1
 AND NOT EXISTS(SELECT 1 FROM acl WHERE grantor<>'control_plane_owner'::regrole OR is_grantable
  OR grantee NOT IN('control_plane_owner'::regrole,'control_plane_migrator'::regrole,'kodex_backup_reader'::regrole)
  OR grantee='kodex_backup_reader'::regrole AND privilege_type NOT IN('SELECT','USAGE'))
 AND (SELECT count(*) FROM acl WHERE grantee='control_plane_owner'::regrole AND privilege_type IN('SELECT','USAGE','UPDATE'))=3
 AND (SELECT count(*) FROM acl WHERE grantee='control_plane_migrator'::regrole AND privilege_type IN('SELECT','USAGE','UPDATE'))=3
 AND (SELECT count(*) FROM acl WHERE grantee='kodex_backup_reader'::regrole) IN(0,2);
