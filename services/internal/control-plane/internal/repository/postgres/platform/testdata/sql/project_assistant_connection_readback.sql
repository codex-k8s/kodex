-- name: project_assistant_connection_readback :one
SELECT (SELECT count(*) FROM control_plane.integration_connections),
       (SELECT count(*) FROM control_plane.project_assistant_connection_purposes),
       (SELECT count(*) FROM control_plane.audit_events),
       (SELECT count(*) FROM control_plane.idempotency_receipts),
       (SELECT count(*) FROM control_plane.outbox_events);
