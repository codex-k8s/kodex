-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='5s';
LOCK TABLE control_plane.session_workspace_storage,control_plane.session_archive_tasks IN ACCESS EXCLUSIVE MODE;

-- Уже установленные guards переходят на ABI5 без изменения ownership, ledger,
-- поколения, positive join и exact source predicates. Данные не переписываются.
-- +goose StatementBegin
DO $workspace_archive_abi$
DECLARE
 v_oid oid:='control_plane.guard_workspace_archive_task()'::regprocedure;
 v_before jsonb; v_after jsonb; v_definition text; v_source text;
 v_old text:='r.safe_snapshot->''roleRuntimeContractRevision''=''4''::jsonb';
 v_new text:='r.safe_snapshot->''roleRuntimeContractRevision''=''5''::jsonb';
BEGIN
 SELECT to_jsonb(p)-'prosrc',pg_get_functiondef(p.oid),p.prosrc
 INTO STRICT v_before,v_definition,v_source FROM pg_proc p
 WHERE p.oid=v_oid AND p.proowner='control_plane_owner'::regrole
  AND NOT p.prosecdef AND p.provolatile='v' AND p.prorettype='trigger'::regtype
  AND p.proconfig=ARRAY['search_path=pg_catalog']
  AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
  AND NOT p.proretset AND p.pronargs=0
  AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
  AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
   WHERE a.grantee NOT IN('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
    OR a.grantee='control_plane_runtime'::regrole AND (a.is_grantable OR a.privilege_type<>'EXECUTE'));
 IF md5(v_source)<>'3655b43600ba95332c11ed4926a2f948'
    OR (length(v_source)-length(replace(v_source,v_old,'')))/length(v_old)<>2 THEN
  RAISE EXCEPTION 'workspace archive runtime ABI predecessor changed';
 END IF;
 EXECUTE replace(v_definition,v_old,v_new);
 SELECT to_jsonb(p)-'prosrc',p.prosrc INTO STRICT v_after,v_source FROM pg_proc p WHERE p.oid=v_oid;
 IF v_after IS DISTINCT FROM v_before OR md5(v_source)<>'18927e175e85b3441e667b62fec8044c' THEN
  RAISE EXCEPTION 'workspace archive runtime ABI attributes changed';
 END IF;
END;
$workspace_archive_abi$;
-- +goose StatementEnd

RESET ROLE;
