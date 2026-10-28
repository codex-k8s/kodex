-- Generated: generate-workspace-backup-compatibility.py; только metadata backup ACL.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
DO $backup_reader_catalog$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='kodex_backup_reader'
  AND rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolinherit AND NOT rolreplication AND rolbypassrls)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_auth_members m JOIN pg_catalog.pg_roles r ON r.oid=m.member WHERE r.rolname='kodex_backup_reader')
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_default_acl d CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) a
  WHERE d.defaclrole='control_plane_owner'::regrole AND d.defaclnamespace='control_plane'::regnamespace
   AND d.defaclobjtype='r' AND a.grantee='kodex_backup_reader'::regrole AND a.privilege_type='SELECT' AND NOT a.is_grantable)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_class r WHERE r.oid IN (pg_catalog.to_regclass('control_plane.session_workspace_storage'),pg_catalog.to_regclass('control_plane.session_archives'),pg_catalog.to_regclass('control_plane.session_archive_tasks'),pg_catalog.to_regclass('control_plane.session_workspace_volumes'),pg_catalog.to_regclass('control_plane.session_workspace_volume_tasks'),pg_catalog.to_regclass('control_plane.session_workspace_volume_attempts'))
  AND NOT EXISTS(SELECT 1 FROM pg_catalog.aclexplode(r.relacl) a
   WHERE a.grantee='kodex_backup_reader'::regrole AND a.privilege_type='SELECT' AND NOT a.is_grantable)) THEN
  RAISE EXCEPTION 'workspace backup reader catalog changed';
 END IF;
END $backup_reader_catalog$;
DO $backup_acl$
DECLARE target text; relation oid;
BEGIN
 FOREACH target IN ARRAY ARRAY['session_workspace_storage','session_archives','session_archive_tasks','session_workspace_volumes','session_workspace_volume_tasks','session_workspace_volume_attempts'] LOOP
  relation:=pg_catalog.to_regclass('control_plane.'||target);
  IF relation IS NULL THEN CONTINUE; END IF;
  IF EXISTS(SELECT 1 FROM pg_catalog.pg_class c WHERE c.oid=relation AND
    (c.relkind<>'r' OR pg_catalog.pg_get_userbyid(c.relowner)<>'control_plane_owner'))
   OR EXISTS(SELECT 1 FROM pg_catalog.pg_class c CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
    WHERE c.oid=relation AND pg_catalog.pg_get_userbyid(a.grantee)='kodex_backup_reader'
      AND (a.privilege_type<>'SELECT' OR a.is_grantable OR pg_catalog.pg_get_userbyid(a.grantor)<>'control_plane_owner'))
   OR EXISTS(SELECT 1 FROM pg_catalog.pg_attribute a WHERE a.attrelid=relation AND a.attnum>0 AND a.attacl IS NOT NULL) THEN
   RAISE EXCEPTION 'workspace backup table authority changed';
  END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM pg_catalog.pg_default_acl d CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) a
  WHERE pg_catalog.pg_get_userbyid(d.defaclrole)='control_plane_owner' AND d.defaclobjtype='r'
   AND pg_catalog.pg_get_userbyid(a.grantee)='kodex_backup_reader'
   AND (d.defaclnamespace<>'control_plane'::regnamespace OR a.privilege_type<>'SELECT' OR a.is_grantable
        OR pg_catalog.pg_get_userbyid(a.grantor)<>'control_plane_owner')) THEN
  RAISE EXCEPTION 'workspace backup default authority changed';
 END IF;
END $backup_acl$;
DO $backup_shape$
DECLARE version bigint; targets integer;
BEGIN
 SELECT max(version_id) INTO version FROM public.goose_db_version WHERE is_applied;
 SELECT count(*) INTO targets FROM pg_catalog.pg_class r WHERE r.oid IN (pg_catalog.to_regclass('control_plane.session_workspace_storage'),pg_catalog.to_regclass('control_plane.session_archives'),pg_catalog.to_regclass('control_plane.session_archive_tasks'),pg_catalog.to_regclass('control_plane.session_workspace_volumes'),pg_catalog.to_regclass('control_plane.session_workspace_volume_tasks'),pg_catalog.to_regclass('control_plane.session_workspace_volume_attempts'));
 IF version IS NULL OR version NOT IN(20261026001500,20261026001600,20261026001700,20261027000100,20261028000100)
 OR targets<>CASE WHEN version IN(20261026001500,20261026001600) THEN 3 ELSE 6 END THEN
  RAISE EXCEPTION 'workspace backup restore shape changed';
 END IF;
END $backup_shape$;
SELECT json_build_object('readOnly',current_setting('transaction_read_only'),'database',current_database(),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),'backupReaderCatalog',true);
ROLLBACK;
