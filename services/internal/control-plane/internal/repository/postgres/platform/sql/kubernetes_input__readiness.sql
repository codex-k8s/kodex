-- name: kubernetes_input__readiness :one
SELECT to_regclass('control_plane.kubernetes_input_stages') IS NOT NULL
 AND to_regprocedure('control_plane.guard_kubernetes_input_stage()') IS NOT NULL
 AND to_regprocedure('control_plane.close_kubernetes_input_stages()') IS NOT NULL
 AND to_regprocedure('control_plane.kubernetes_execution_child_grant_current(uuid,uuid)') IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM(VALUES('sequence','bigint'),('byte_count','integer'),('delivery_expires_at','timestamp with time zone'),('native_admitted_at','timestamp with time zone'),('acknowledged_at','timestamp with time zone')) required(column_name,type_name)
  WHERE NOT EXISTS(SELECT 1 FROM pg_attribute attribute WHERE attribute.attrelid=to_regclass('control_plane.kubernetes_input_stages')
   AND attribute.attname=required.column_name AND attribute.attnum>0 AND NOT attribute.attisdropped AND format_type(attribute.atttypid,attribute.atttypmod)=required.type_name))
 AND NOT EXISTS(SELECT 1 FROM(VALUES('kubernetes_input_stage_guard','control_plane.kubernetes_input_stages'),('kubernetes_input_stage_version','control_plane.kubernetes_input_stages'),('kubernetes_input_stream_closure','control_plane.kubernetes_execution_streams')) required(trigger_name,relation_name)
  WHERE NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass(required.relation_name) AND tgname=required.trigger_name AND tgenabled='O'))
 AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['SELECT','INSERT','UPDATE']) required(privilege_name) WHERE NOT has_table_privilege(session_user,'control_plane.kubernetes_input_stages',required.privilege_name));
