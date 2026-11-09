-- ABI заменяемых функций: тело меняется миграцией, интерфейс и права сохраняются.
SELECT CASE WHEN count(*) = 5 THEN jsonb_agg(jsonb_build_object(
    'oid', p.oid, 'owner', p.proowner, 'language', p.prolang, 'kind', p.prokind,
    'argumentTypes', p.proargtypes::text, 'allArgumentTypes', p.proallargtypes,
    'argumentNames', p.proargnames, 'argumentModes', p.proargmodes,
    'resultType', p.prorettype, 'returnsSet', p.proretset, 'defaults', p.pronargdefaults,
    'volatility', p.provolatile, 'securityDefiner', p.prosecdef,
    'strict', p.proisstrict, 'leakproof', p.proleakproof, 'parallel', p.proparallel,
    'configuration', p.proconfig, 'acl', p.proacl::text
) ORDER BY p.oid) END
FROM pg_catalog.pg_proc p WHERE p.oid IN (
    'control_plane.guard_personal_invocation_credential()'::regprocedure,
    'control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])'::regprocedure,
    'control_plane.protect_email_mailbox_gate()'::regprocedure,
    'control_plane.revoke_integration_approval_scopes()'::regprocedure,
    'control_plane.guard_integration_approval_scope_update()'::regprocedure
);
