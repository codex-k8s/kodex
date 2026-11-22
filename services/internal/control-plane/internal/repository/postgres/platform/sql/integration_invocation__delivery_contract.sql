-- name: integration_invocation__delivery_contract :one
SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_attribute
 WHERE attrelid=to_regclass('control_plane.integration_invocations') AND attname='kubernetes_claimed_instance' AND NOT attisdropped)
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid=to_regclass('control_plane.integration_invocations')
 AND tgname='kubernetes_claimed_instance_guard' AND tgenabled='O')
 AND to_regclass('control_plane.kubernetes_execution_completions') IS NOT NULL
 AND has_table_privilege(session_user,'control_plane.integration_invocations','SELECT') AND has_table_privilege(session_user,'control_plane.integration_invocations','UPDATE')
 AND has_table_privilege(session_user,'control_plane.integration_effect_receipts','SELECT') AND has_table_privilege(session_user,'control_plane.integration_effect_receipts','INSERT');
