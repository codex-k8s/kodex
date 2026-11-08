SELECT (SELECT count(*) FROM control_plane.image_builds),
 (SELECT count(*) FROM control_plane.image_vulnerability_reports),
 (SELECT count(*) FROM control_plane.idempotency_receipts),
 (SELECT count(*) FROM control_plane.outbox_events),
 (SELECT platform_sequence FROM control_plane.installation WHERE singleton),
 (SELECT md5(COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.ref)::text,'[]')) FROM control_plane.role_image_recipes r),
 (SELECT md5(COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.ref)::text,'[]')) FROM control_plane.image_artifacts a),
 (SELECT md5(COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.ref)::text,'[]')) FROM control_plane.image_admission_attempts a)
