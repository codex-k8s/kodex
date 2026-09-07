\set ON_ERROR_STOP on
BEGIN READ ONLY;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '2s';
SET LOCAL row_security = off;
-- Оператор читает всю owner DB, а не результат tenant RLS.
SELECT (session_user = current_user AND EXISTS (
  SELECT 1 FROM pg_roles WHERE rolname = session_user AND rolsuper
)) AS global_reader \gset
\if :global_reader
WITH target AS (
  SELECT :'secret_ref'::text AS ref, :'secret_name'::text AS name,
         :'secret_uid'::text AS uid, :'operation_ref'::text AS operation
), strings AS (
  SELECT jsonb_build_array(ref, name, uid) AS values FROM target
), direct_refs AS (
 SELECT 1 FROM control_plane.runtime_secrets s,target t WHERE s.ref=t.ref
 UNION ALL SELECT 1 FROM control_plane.runtime_secret_revisions r,target t
   WHERE r.secret_uid::text=t.uid OR r.secret_name=t.name
 UNION ALL SELECT 1 FROM control_plane.runtime_secret_operations o,target t
   WHERE o.ref=t.operation
 UNION ALL SELECT 1 FROM control_plane.runtime_secret_draft_operations o,target t
   WHERE o.ref=t.operation
 UNION ALL SELECT 1 FROM control_plane.provider_credential_revisions r,target t WHERE r.secret_uid::text=t.uid
 UNION ALL SELECT 1 FROM control_plane.provider_credential_cleanup_tasks r,target t WHERE r.secret_uid::text=t.uid
 UNION ALL SELECT 1 FROM control_plane.integration_credential_revisions r,target t WHERE r.secret_uid::text=t.uid
 UNION ALL SELECT 1 FROM control_plane.email_credential_descriptors r,target t WHERE r.secret_uid::text=t.uid
 UNION ALL SELECT 1 FROM control_plane.runtime_revisions r,target t WHERE r.provider_secret_uid::text=t.uid
), snapshots AS (
 SELECT terminal_secret_snapshot AS value FROM control_plane.runtime_secret_operations
 UNION ALL SELECT encrypted_descriptor FROM control_plane.runtime_secret_drafts
 UNION ALL SELECT terminal_snapshot FROM control_plane.runtime_secret_draft_operations
 UNION ALL SELECT encrypted_cleanup_descriptor FROM control_plane.runtime_secret_draft_operations
 UNION ALL SELECT materialization_cleanup_descriptor FROM control_plane.runtime_secret_draft_operations
 UNION ALL SELECT secret_descriptors FROM control_plane.runtime_environment_versions
 UNION ALL SELECT specification FROM control_plane.runtime_environment_drafts
 UNION ALL SELECT safe_snapshot FROM control_plane.runtime_revisions
 UNION ALL SELECT snapshot FROM control_plane.runtime_secret_draft_impact_items
)
SELECT EXISTS(SELECT 1 FROM direct_refs) OR EXISTS(
 SELECT 1 FROM snapshots s,strings t
 WHERE jsonb_path_exists(s.value, 'strict $.** ? (@.type() == "string" && @ == $targets[*])',
   jsonb_build_object('targets',t.values))
) AS blocked;
\else
-- psql \quit не задаёт exit status; явная SQL ошибка закрывает этот путь.
SELECT 1 / 0 AS global_reader_required;
\endif
ROLLBACK;
