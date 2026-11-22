-- name: kubernetes_capture__readiness :one
SELECT
 to_regprocedure('control_plane.kubernetes_capture_claim(text,integer,integer)') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_capture_consumer_heartbeat(text,integer)') IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM(VALUES('version'),('repair_generation'),('repair_base_attempt_count'),('attempt_count')) required(column_name)
  WHERE NOT EXISTS(SELECT 1 FROM pg_attribute attribute
   WHERE attribute.attrelid='control_plane.kubernetes_output_captures'::regclass AND attribute.attname=required.column_name
    AND attribute.attnum>0 AND NOT attribute.attisdropped AND attribute.attnotnull AND attribute.atttypid='bigint'::regtype))
 AND to_regprocedure('control_plane.kubernetes_stage_cleanup_claim(text,integer,integer)') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_stage_cleanup_complete(uuid,text,text,bigint,text,text,boolean)') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_stage_cleanup_status(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('control_plane.repair_kubernetes_stage_cleanup(uuid,uuid,bigint)') IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM(VALUES('control_plane.kubernetes_input_stages'),('control_plane.kubernetes_output_stages'),('control_plane.prepared_content')) required(relation_name)
  CROSS JOIN(VALUES('cleanup_total_attempts'),('cleanup_repair_generation')) field(column_name)
  WHERE NOT EXISTS(SELECT 1 FROM pg_attribute attribute WHERE attribute.attrelid=required.relation_name::regclass
   AND attribute.attname=field.column_name AND attribute.attnum>0 AND NOT attribute.attisdropped
   AND attribute.attnotnull AND attribute.atttypid='bigint'::regtype))
 AND to_regprocedure('control_plane.kubernetes_capture_artifact_visible(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_capture_runtime_visible(uuid,uuid,uuid,uuid,text)') IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM(VALUES
  ('control_plane.kubernetes_output_captures','SELECT'),('control_plane.kubernetes_output_captures','UPDATE'),
  ('control_plane.kubernetes_capture_attempts','SELECT'),('control_plane.kubernetes_capture_attempts','UPDATE'),
  ('control_plane.kubernetes_capture_artifact_heads','SELECT'),('control_plane.kubernetes_capture_artifact_heads','INSERT'),('control_plane.kubernetes_capture_artifact_heads','UPDATE'),
  ('control_plane.kubernetes_capture_artifact_revisions','SELECT'),('control_plane.kubernetes_capture_artifact_revisions','INSERT'),
  ('control_plane.prepared_content','SELECT'),('control_plane.prepared_content','INSERT'),('control_plane.prepared_content','UPDATE'),
  ('control_plane.artifact_heads','SELECT'),('control_plane.artifact_heads','INSERT'),('control_plane.artifact_heads','UPDATE'),
  ('control_plane.artifact_revisions','SELECT'),('control_plane.artifact_revisions','INSERT'),
  ('control_plane.artifact_revision_content','SELECT'),('control_plane.artifact_revision_content','INSERT')) required(relation_name,privilege_name)
  WHERE NOT has_table_privilege(session_user,required.relation_name,required.privilege_name))
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='control_plane.kubernetes_execution_streams'::regclass AND tgname='kubernetes_capture_stream_terminal' AND tgenabled='O')
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='control_plane.kubernetes_execution_frames'::regclass AND tgname='kubernetes_capture_late_output_ack' AND tgenabled='O')
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='control_plane.prepared_content'::regclass AND conname='prepared_content_source_profile_check' AND convalidated AND position('KUBERNETES_OUTPUT' IN pg_get_constraintdef(oid))>0);
