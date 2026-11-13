-- name: workspace_storage__readiness :one
SELECT EXISTS (
    SELECT 1 FROM pg_catalog.pg_attribute attribute
    WHERE attribute.attrelid=pg_catalog.to_regclass('control_plane.session_workspace_storage')
        AND attribute.attname='workflow_version_id' AND NOT attribute.attisdropped
        AND attribute.atttypid='uuid'::regtype
) AND COALESCE(pg_catalog.has_function_privilege(current_user,
    pg_catalog.to_regprocedure('control_plane.workspace_session_scope_matches(uuid,uuid,uuid)'), 'EXECUTE'), false) AND EXISTS (
 SELECT 1 FROM pg_catalog.pg_class receipt
 WHERE receipt.oid=pg_catalog.to_regclass('control_plane.workspace_unbound_stops')
 AND receipt.relowner='control_plane_owner'::regrole
) AND COALESCE(pg_catalog.has_table_privilege(current_user,
 pg_catalog.to_regclass('control_plane.workspace_unbound_stops'),'SELECT'),false)
AND COALESCE(pg_catalog.has_table_privilege(current_user,
 pg_catalog.to_regclass('control_plane.workspace_unbound_stops'),'INSERT'),false)
AND EXISTS (SELECT 1 FROM pg_catalog.pg_trigger trigger
 WHERE trigger.tgrelid=pg_catalog.to_regclass('control_plane.workspace_unbound_stops')
 AND trigger.tgfoid=pg_catalog.to_regprocedure('control_plane.guard_workspace_unbound_stop()')
 AND trigger.tgenabled='O' AND NOT trigger.tgisinternal);
