SELECT
 (SELECT count(*) FROM control_plane.assistant_plans WHERE operations::text LIKE '%'||$1||'%')+
 (SELECT count(*) FROM control_plane.assistant_plan_revisions WHERE operations::text LIKE '%'||$1||'%')+
 (SELECT count(*) FROM control_plane.idempotency_receipts WHERE response_payload::text LIKE '%'||$1||'%')+
 (SELECT count(*) FROM control_plane.audit_events WHERE row_to_json(audit_events)::text LIKE '%'||$1||'%')+
 (SELECT count(*) FROM control_plane.outbox_events WHERE row_to_json(outbox_events)::text LIKE '%'||$1||'%');
