-- Frozen015 archive/task metadata; no business rows.
DO $checkpoint_catalog$
DECLARE v_actual jsonb; v_path text;
BEGIN
 v_path:=current_setting('search_path'); PERFORM set_config('search_path','pg_catalog',true);
WITH targets AS (
 SELECT c.* FROM pg_class c WHERE c.oid IN ('control_plane.session_archives'::regclass,'control_plane.session_archive_tasks'::regclass)
), catalogs AS (
 SELECT r.relname,jsonb_build_object(
 'relation',jsonb_build_array(pg_get_userbyid(r.relowner),r.relkind,r.relpersistence,r.relrowsecurity,r.relforcerowsecurity,r.relispartition,
   EXISTS(SELECT 1 FROM pg_inherits WHERE inhrelid=r.oid OR inhparent=r.oid)),
 'columns',(SELECT jsonb_agg(jsonb_build_array(a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,pg_get_expr(d.adbin,d.adrelid),a.attidentity,a.attgenerated,a.attacl::text) ORDER BY a.attnum)
   FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=r.oid AND a.attnum>0 AND NOT a.attisdropped),
 'constraints',(SELECT jsonb_agg(jsonb_build_array(c.conname,c.contype,c.convalidated,c.conenforced,c.condeferrable,c.condeferred,pg_get_constraintdef(c.oid)) ORDER BY c.conname)
   FROM pg_constraint c WHERE c.conrelid=r.oid),
 'indexes',(SELECT jsonb_agg(jsonb_build_array(c.relname,i.indisunique,i.indisvalid,i.indisready,pg_get_indexdef(i.indexrelid)) ORDER BY c.relname)
   FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE i.indrelid=r.oid),
 'triggers',(SELECT jsonb_agg(jsonb_build_array(CASE WHEN t.tgisinternal THEN regexp_replace(t.tgname,'_[0-9]+$','_<oid>') ELSE t.tgname END,
   t.tgtype,t.tgenabled,t.tgisinternal,t.tgdeferrable,t.tginitdeferred,t.tgnargs,t.tgattr::text,pg_get_expr(t.tgqual,t.tgrelid),t.tgfoid::regprocedure::text,
   c.conname,c.conrelid::regclass::text,c.confrelid::regclass::text,t.tgparentid=0,encode(t.tgargs,'hex'),t.tgoldtable,t.tgnewtable)
   ORDER BY t.tgisinternal,coalesce(c.conrelid::regclass::text,''),coalesce(c.conname,''),t.tgfoid::regprocedure::text,t.tgtype)
   FROM pg_trigger t LEFT JOIN pg_constraint c ON c.oid=t.tgconstraint WHERE t.tgrelid=r.oid),
 'functions',(SELECT jsonb_agg(jsonb_build_array(p.oid::regprocedure::text,md5(p.prosrc),pg_get_userbyid(p.proowner),p.prosecdef,p.provolatile,p.proconfig,p.prorettype::regtype::text,l.lanname,
   (SELECT jsonb_agg(jsonb_build_array(pg_get_userbyid(a.grantee),a.privilege_type,a.is_grantable) ORDER BY pg_get_userbyid(a.grantee),a.privilege_type)
     FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a)) ORDER BY p.oid::regprocedure::text)
   FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang WHERE p.oid IN(SELECT tgfoid FROM pg_trigger WHERE tgrelid=r.oid AND NOT tgisinternal)),
 'acl',(SELECT jsonb_agg(jsonb_build_array(pg_get_userbyid(a.grantee),a.privilege_type,a.is_grantable) ORDER BY pg_get_userbyid(a.grantee),a.privilege_type)
   FROM aclexplode(coalesce(r.relacl,acldefault('r',r.relowner))) a)
 ) AS metadata FROM targets r
)
SELECT jsonb_build_object('catalog',jsonb_object_agg(relname,metadata),
 'fingerprints',jsonb_object_agg(relname,(SELECT jsonb_object_agg(key,md5(value::text)) FROM jsonb_each(metadata)))) FROM catalogs INTO v_actual;
 PERFORM set_config('search_path',v_path,true);
 IF v_actual->'fingerprints' IS DISTINCT FROM '{"session_archive_tasks":{"acl":"4a534d150446bd64168071defd5f0aa7","columns":"f3cbc24750758ad40dd5f8c502d23dbe","constraints":"1be4f694414c710b9d5c7af598cd27db","functions":"37a6259cc0c1dae299a7866489dff0bd","indexes":"4cf010f3fd87ac462843d83d003726b4","relation":"cdefe876d5d2436e0650dc9eda73dca7","triggers":"8331df0639de4e264fc648e83bbcace3"},"session_archives":{"acl":"4a534d150446bd64168071defd5f0aa7","columns":"aeff8a879216d033e5511dd8a5f38236","constraints":"ecdd38f111e0e01fd0a771f2c7e80663","functions":"9cd820ff9c9e844b48fab3640f34a7f6","indexes":"b58c0b62df318e3d1c22c387e8696a31","relation":"cdefe876d5d2436e0650dc9eda73dca7","triggers":"a7c1b6f4b0ab976bdbb1fc7984050242"}}'::jsonb THEN
   RAISE EXCEPTION 'workspace checkpoint predecessor archive catalog changed';
 END IF;
END $checkpoint_catalog$;
