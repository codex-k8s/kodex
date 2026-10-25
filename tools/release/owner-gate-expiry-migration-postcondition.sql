-- name: owner_gate_expiry_migration_postcondition :one
-- Только schema/authority metadata. Сравнение CHECK удаляет лишь оформление pg_get_constraintdef.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
WITH checks AS (
    SELECT conname, regexp_replace(replace(pg_get_constraintdef(oid),'::text',''),'[[:space:]()]','','g') AS definition
    FROM pg_constraint WHERE conrelid='control_plane.owner_gates'::regclass AND contype='c' AND convalidated
), guard AS (
    SELECT p.oid, p.prosrc, p.prosecdef, pg_get_userbyid(p.proowner) AS owner, p.proacl
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
        AND definition=$expected$CHECKstate=ANYARRAY['OPEN','APPROVED','REJECTED','CHANGES_REQUESTED','CANCELLED','EXPIRED']$expected$),
    'resolutionConstraint',EXISTS (SELECT 1 FROM checks WHERE conname='owner_gates_resolution_check'
        AND definition=$expected$CHECKstate='OPEN'ANDdecisionISNULLANDresolved_byISNULLANDresolved_atISNULLORstate='EXPIRED'ANDdecisionISNULLANDresolved_byISNULLANDresolved_atISNOTNULLANDexpires_atISNOTNULLANDintegration_invocation_idISNULLORstate=ANYARRAY['APPROVED','REJECTED','CHANGES_REQUESTED','CANCELLED']ANDdecisionISNOTNULLANDresolved_byISNOTNULLANDresolved_atISNOTNULL$expected$),
    'expiryConstraint',EXISTS (SELECT 1 FROM checks WHERE conname='owner_gates_expiry_check'
        AND definition=$expected$CHECKexpires_atISNULLORexpires_at>created_atANDintegration_invocation_idISNULL$expected$),
    'dueIndex',EXISTS (SELECT 1 FROM pg_index i WHERE i.indexrelid=to_regclass('control_plane.owner_gates_due_expiry')
        AND i.indrelid='control_plane.owner_gates'::regclass AND i.indisvalid AND i.indisready AND NOT i.indisunique
        AND i.indnatts=3 AND i.indnkeyatts=3
        AND pg_get_indexdef(i.indexrelid,1,true)='organization_id'
        AND pg_get_indexdef(i.indexrelid,2,true)='expires_at' AND pg_get_indexdef(i.indexrelid,3,true)='id'
        AND regexp_replace(replace(pg_get_expr(i.indpred,i.indrelid),'::text',''),'[[:space:]()]','','g')=$expected$state='OPEN'ANDexpires_atISNOTNULL$expected$),
    'immutableGuard',EXISTS (SELECT 1 FROM pg_trigger t JOIN guard g ON g.oid=t.tgfoid
        WHERE t.tgrelid='control_plane.owner_gates'::regclass AND t.tgname='owner_gate_expiry_immutable'
            AND t.tgenabled='O' AND t.tgtype=19 AND NOT t.tgisinternal AND t.tgqual IS NULL AND t.tgnargs=0
            AND NOT g.prosecdef AND g.owner='control_plane_owner'
            AND regexp_replace(g.prosrc,'[[:space:]]','','g')=regexp_replace($body$
BEGIN
    IF NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'owner gate expiry is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$body$,'[[:space:]]','','g')),
    'runtimeExecute',EXISTS (SELECT 1 FROM guard WHERE has_function_privilege('control_plane_runtime',oid,'EXECUTE')),
    'publicExecuteRevoked',EXISTS (SELECT 1 FROM guard g WHERE NOT EXISTS (
        SELECT 1 FROM aclexplode(coalesce(g.proacl,acldefault('f',(SELECT oid FROM pg_roles WHERE rolname=g.owner)))) a
        WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
)::text;
ROLLBACK;
