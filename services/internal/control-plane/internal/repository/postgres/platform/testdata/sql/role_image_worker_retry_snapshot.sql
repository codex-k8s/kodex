SELECT version, (SELECT count(*) FROM control_plane.idempotency_receipts),
 (SELECT count(*) FROM control_plane.outbox_events), (SELECT count(*) FROM control_plane.image_artifacts),
 (SELECT count(*) FROM control_plane.audit_events),
 (SELECT platform_sequence FROM control_plane.installation WHERE singleton)
 FROM control_plane.image_builds WHERE ref=$1
