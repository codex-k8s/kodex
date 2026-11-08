-- Шаблон exact036: generator подставляет только закрытый ACL profile и MD5 authored body.
-- Не выдаёт бизнес-строки, значения watermark либо credentials.
DO $authority_counter_catalog$
DECLARE
 v_relation oid := to_regclass('control_plane.authority_proof_revision_counter');
 v_function oid := to_regprocedure('control_plane.reject_legacy_authority_proof_counter()');
 v_valid boolean;
BEGIN
 SELECT EXISTS(SELECT 1 FROM pg_class r WHERE r.oid=v_relation
   AND r.relkind='r' AND r.relpersistence='p' AND NOT r.relispartition
   AND r.relowner='control_plane_owner'::regrole AND r.relam=(SELECT oid FROM pg_am WHERE amname='heap')
   AND r.reltablespace=0 AND r.reloptions IS NULL AND NOT r.relrowsecurity AND NOT r.relforcerowsecurity
   AND r.relreplident='d')
 AND (SELECT jsonb_agg(jsonb_build_array(a.attnum,a.attname,format_type(a.atttypid,a.atttypmod),
   a.attnotnull,a.atthasdef,a.attidentity,a.attgenerated,a.attisdropped,a.attacl::text) ORDER BY a.attnum)
   FROM pg_attribute a WHERE a.attrelid=v_relation AND a.attnum>0)
   = '[[1,"singleton","boolean",true,false,"","",false,null],[2,"revision","bigint",true,false,"","",false,null]]'::jsonb
 AND (SELECT jsonb_object_agg(c.conname,jsonb_build_array(c.contype,c.convalidated,c.conenforced,
   c.condeferrable,c.condeferred,c.conislocal,c.coninhcount,c.connoinherit,pg_get_constraintdef(c.oid)))
   FROM pg_constraint c WHERE c.conrelid=v_relation)
   = $counter_constraints$@CONSTRAINTS@$counter_constraints$::jsonb
 AND NOT EXISTS(SELECT 1 FROM pg_constraint WHERE confrelid=v_relation)
 AND (SELECT count(*)=1 AND bool_and(i.indisunique AND i.indisprimary AND i.indisvalid AND i.indisready
   AND i.indislive AND i.indpred IS NULL AND i.indexprs IS NULL
   AND pg_get_indexdef(i.indexrelid)='CREATE UNIQUE INDEX authority_proof_revision_counter_pkey ON control_plane.authority_proof_revision_counter USING btree (singleton)')
   FROM pg_index i WHERE i.indrelid=v_relation)
 AND NOT EXISTS(SELECT 1 FROM pg_inherits WHERE inhrelid=v_relation OR inhparent=v_relation)
 AND NOT EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=v_relation)
 AND NOT EXISTS(SELECT 1 FROM pg_rewrite WHERE ev_class=v_relation)
 AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=v_relation)
 AND NOT EXISTS(SELECT 1 FROM pg_class WHERE relnamespace='control_plane'::regnamespace
   AND relkind='S' AND relname LIKE 'authority_proof_revision_counter%')
 AND (SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,
   pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable)
   ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),a.privilege_type)
   FROM pg_class r CROSS JOIN LATERAL aclexplode(coalesce(r.relacl,acldefault('r',r.relowner))) a
   WHERE r.oid=v_relation @ACL_FILTER@) = $counter_acl$@ACL@$counter_acl$::jsonb
 AND @OPTIONAL_BACKUP_GUARD@
 AND (SELECT count(*)=1 FROM pg_proc WHERE pronamespace='control_plane'::regnamespace
   AND proname='reject_legacy_authority_proof_counter')
 AND EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=v_function
   AND p.proowner='control_plane_owner'::regrole AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
   AND NOT p.prosecdef AND p.prokind='f' AND p.provolatile='v' AND p.proparallel='u'
   AND NOT p.proisstrict AND NOT p.proleakproof AND NOT p.proretset AND p.prorettype='trigger'::regtype
   AND p.pronargs=0 AND p.pronargdefaults=0 AND p.proargnames IS NULL AND p.proargmodes IS NULL
   AND p.proconfig=ARRAY['search_path=pg_catalog'] AND md5(p.prosrc)='@FUNCTION_MD5@'
   AND (SELECT jsonb_agg(jsonb_build_array(CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,
     pg_get_userbyid(a.grantor),a.privilege_type,a.is_grantable)
     ORDER BY a.grantee=0,pg_get_userbyid(a.grantee),a.privilege_type)
     FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a)
     = '[["control_plane_owner","control_plane_owner","EXECUTE",false],["control_plane_runtime","control_plane_owner","EXECUTE",false]]'::jsonb)
 AND (SELECT count(*)=1 AND bool_and(t.tgrelid='control_plane.installation'::regclass
   AND t.tgname='reject_legacy_authority_proof_counter' AND t.tgfoid=v_function
   AND t.tgtype=19 AND t.tgenabled='O' AND NOT t.tgisinternal AND t.tgparentid=0
   AND t.tgconstraint=0 AND t.tgconstrrelid=0 AND t.tgconstrindid=0
   AND NOT t.tgdeferrable AND NOT t.tginitdeferred AND t.tgnargs=0 AND octet_length(t.tgargs)=0
   AND t.tgqual IS NULL AND t.tgoldtable IS NULL AND t.tgnewtable IS NULL
   AND t.tgattr::text=(SELECT attnum::text FROM pg_attribute WHERE attrelid='control_plane.installation'::regclass
     AND attname='authority_proof_sequence' AND NOT attisdropped))
   FROM pg_trigger t WHERE t.tgfoid=v_function OR
     (t.tgrelid='control_plane.installation'::regclass AND t.tgname='reject_legacy_authority_proof_counter'))
 INTO v_valid;
 IF v_valid IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'authority proof counter schema catalog changed';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM control_plane.authority_proof_revision_counter c
   JOIN control_plane.installation i ON i.singleton=c.singleton
   WHERE c.singleton AND c.revision>=i.authority_proof_sequence AND c.revision<=9007199254740991) THEN
   RAISE EXCEPTION 'authority proof counter watermark is unavailable';
 END IF;
END $authority_counter_catalog$;
