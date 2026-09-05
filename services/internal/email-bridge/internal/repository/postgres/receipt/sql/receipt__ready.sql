-- name: receipt__ready :one
SELECT session_user = 'email_bridge_runtime'
AND NOT (SELECT rolbypassrls OR rolsuper FROM pg_roles WHERE rolname=session_user)
AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid='email_bridge.receipts'::regclass)
AND has_table_privilege(session_user, 'email_bridge.receipts','SELECT,INSERT,UPDATE')
AND NOT has_table_privilege(session_user, 'email_bridge.receipts','DELETE')
AND (SELECT count(*)>=0 FROM email_bridge.receipts WHERE report_pending)
AND (SELECT count(*) >= 0 FROM email_bridge.configuration_watermark)
AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid='email_bridge.owner_receipts'::regclass)
AND has_table_privilege(session_user, 'email_bridge.owner_receipts','SELECT,INSERT,UPDATE')
AND NOT has_table_privilege(session_user, 'email_bridge.owner_receipts','DELETE');
