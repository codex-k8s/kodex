BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','ARTIFACT_OPTIONAL_ANTIVIRUS_V1',
 'database',current_database(), 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'constraintsValidated',(SELECT count(*)=4 AND bool_and(convalidated)
   FROM pg_constraint WHERE (conrelid,conname) IN (
    ('control_plane.artifact_revisions'::regclass,'artifact_revisions_scan_state_check'),
    ('control_plane.prepared_content'::regclass,'prepared_content_scan_state_check'),
    ('control_plane.artifact_revision_content'::regclass,'artifact_antivirus_receipt_shape'),
    ('control_plane.prepared_content'::regclass,'prepared_antivirus_receipt_shape'))),
 'explicitNoScanReceipt',control_plane.artifact_scan_receipt_valid(
  '{"engine":"format-policy-v1","policy":"artifact-format-only-v1","checkedAt":"2026-01-01T00:00:00Z","verdict":"NOT_SCANNED"}'::jsonb),
 'noForgedClean',NOT control_plane.artifact_scan_receipt_valid(
  '{"engine":"format-policy-v1","policy":"artifact-format-only-v1","checkedAt":"2026-01-01T00:00:00Z","verdict":"CLEAN"}'::jsonb),
 'realScanReceipt',control_plane.artifact_scan_receipt_valid(
  '{"engine":"ClamAV synthetic/fixture","policy":"clamav-instream-v1","scannedAt":"2026-01-01T00:00:00Z","verdict":"CLEAN"}'::jsonb),
 'noAbsentReceipt',NOT control_plane.artifact_scan_receipt_valid(NULL)
)::text;
ROLLBACK;
