-- name: owner_gate_expiry_migration_postcondition :one
-- Только schema/authority metadata. Сравнение CHECK сохраняет группировку pg_get_constraintdef закреплённого PostgreSQL18.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
WITH checks AS (
    SELECT conname, regexp_replace(replace(pg_get_constraintdef(oid),'::text',''),'[[:space:]]','','g') AS definition
    FROM pg_constraint WHERE conrelid='control_plane.owner_gates'::regclass AND contype='c' AND convalidated
), guard AS (
    SELECT p.oid, p.prosrc, p.prosecdef, p.prorettype, p.prolang, pg_get_userbyid(p.proowner) AS owner, p.proacl
    FROM pg_proc p WHERE p.oid=to_regprocedure('control_plane.guard_owner_gate_expiry()')
)
SELECT jsonb_build_object(
    'readOnly',current_setting('transaction_read_only'),
    'database',current_database(),
    'gooseVersion',(SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),
    'tableOwner',(SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid='control_plane.owner_gates'::regclass),
    'expiryColumn',EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid='control_plane.owner_gates'::regclass
        AND attname='expires_at' AND atttypid='timestamptz'::regtype AND NOT attnotnull AND NOT atthasdef AND NOT attisdropped),
    'stateConstraint',EXISTS (SELECT 1 FROM checks WHERE conname='owner_gates_state_check'
        AND definition=$expected$CHECK((state=ANY(ARRAY['OPEN','APPROVED','REJECTED','CHANGES_REQUESTED','CANCELLED','EXPIRED'])))$expected$),
    'resolutionConstraint',EXISTS (SELECT 1 FROM checks WHERE conname='owner_gates_resolution_check'
        AND definition=$expected$CHECK((((state='OPEN')AND(decisionISNULL)AND(resolved_byISNULL)AND(resolved_atISNULL))OR((state='EXPIRED')AND(decisionISNULL)AND(resolved_byISNULL)AND(resolved_atISNOTNULL)AND(expires_atISNOTNULL)AND(integration_invocation_idISNULL))OR((state=ANY(ARRAY['APPROVED','REJECTED','CHANGES_REQUESTED','CANCELLED']))AND(decisionISNOTNULL)AND(resolved_byISNOTNULL)AND(resolved_atISNOTNULL))))$expected$),
    'expiryConstraint',EXISTS (SELECT 1 FROM checks WHERE conname='owner_gates_expiry_check'
        AND definition=$expected$CHECK(((expires_atISNULL)OR((expires_at>created_at)AND(integration_invocation_idISNULL))))$expected$),
    'dueIndex',EXISTS (SELECT 1 FROM pg_index i WHERE i.indexrelid=to_regclass('control_plane.owner_gates_due_expiry')
        AND i.indrelid='control_plane.owner_gates'::regclass AND i.indisvalid AND i.indisready AND NOT i.indisunique
        AND i.indnatts=3 AND i.indnkeyatts=3
        AND pg_get_indexdef(i.indexrelid,1,true)='organization_id'
        AND pg_get_indexdef(i.indexrelid,2,true)='expires_at' AND pg_get_indexdef(i.indexrelid,3,true)='id'
        AND regexp_replace(replace(pg_get_expr(i.indpred,i.indrelid),'::text',''),'[[:space:]]','','g')=$expected$((state='OPEN')AND(expires_atISNOTNULL))$expected$),
    'immutableGuard',EXISTS (SELECT 1 FROM pg_trigger t JOIN guard g ON g.oid=t.tgfoid
        WHERE t.tgrelid='control_plane.owner_gates'::regclass AND t.tgname='owner_gate_expiry_immutable'
            AND t.tgattr=''::int2vector AND t.tgenabled='O' AND t.tgtype=19 AND NOT t.tgisinternal AND t.tgqual IS NULL AND t.tgnargs=0
            AND NOT g.prosecdef AND g.owner='control_plane_owner'
            AND g.prorettype='trigger'::regtype AND g.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
            AND g.prosrc=$body$
BEGIN
    IF NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'owner gate expiry is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$body$),
    'runtimeExecute',EXISTS (SELECT 1 FROM guard WHERE has_function_privilege('control_plane_runtime',oid,'EXECUTE')),
    'publicExecuteRevoked',EXISTS (SELECT 1 FROM guard g WHERE NOT EXISTS (
        SELECT 1 FROM aclexplode(coalesce(g.proacl,acldefault('f',(SELECT oid FROM pg_roles WHERE rolname=g.owner)))) a
        WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
)::text;
ROLLBACK;
