-- name: ssh_execution__contract :one
WITH relations(name,privileges) AS (VALUES
 ('ssh_identity_setups','SELECT,INSERT,UPDATE'),
 ('ssh_identity_upload_attempts','SELECT,INSERT,UPDATE'),
 ('ssh_identity_upload_observations','SELECT,INSERT'),
 ('ssh_identity_material_revisions','SELECT,INSERT'),
 ('ssh_identity_ack_rejections','SELECT,INSERT'),
 ('ssh_identity_quarantines','SELECT,INSERT'),
 ('ssh_identity_health_probes','SELECT,INSERT,UPDATE'),
 ('ssh_invocation_identity_bindings','SELECT,INSERT'),
 ('ssh_invocation_claim_cursors','SELECT,INSERT,UPDATE'),
 ('ssh_execution_intents','SELECT,INSERT'),
 ('ssh_execution_observations','SELECT,INSERT')),
required_triggers(relation_name,trigger_name) AS (VALUES
 ('ssh_identity_upload_observations','ssh_identity_observations_immutable'),
 ('ssh_identity_material_revisions','ssh_identity_material_immutable'),
 ('ssh_identity_ack_rejections','ssh_identity_rejections_immutable'),
 ('ssh_identity_quarantines','ssh_identity_quarantines_immutable'),
 ('ssh_identity_health_probes','ssh_health_receipt_immutable'),
 ('ssh_invocation_identity_bindings','ssh_invocation_identity_owner'),
 ('ssh_execution_intents','ssh_execution_intent_owner')),
required_project_triggers(relation_name,immutable_name,purge_name,key_name) AS (VALUES
 ('ssh_invocation_identity_bindings','ssh_invocation_identity_immutable','ssh_invocation_identity_purge_only','invocation_id'),
 ('ssh_execution_intents','ssh_execution_intent_immutable','ssh_execution_intent_purge_only','id'),
 ('ssh_execution_observations','ssh_execution_observation_immutable','ssh_execution_observation_purge_only','intent_id'))
SELECT NOT EXISTS(SELECT 1 FROM relations
 CROSS JOIN LATERAL (SELECT to_regclass('control_plane.'||name) AS relation,
  string_to_array(privileges,',') AS allowed) scope
 WHERE scope.relation IS NULL
  OR EXISTS(SELECT 1 FROM unnest(scope.allowed) AS required_privilege(value)
   WHERE NOT coalesce(has_table_privilege(session_user,scope.relation,required_privilege.value),false))
  OR has_table_privilege(session_user,scope.relation,'DELETE,TRUNCATE,TRIGGER,MAINTAIN')
  OR has_any_column_privilege(session_user,scope.relation,'REFERENCES')
  OR (NOT ('UPDATE'=ANY(scope.allowed)) AND has_any_column_privilege(session_user,scope.relation,'UPDATE')))
 AND NOT EXISTS(SELECT 1 FROM required_triggers required
  WHERE NOT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger owner_trigger
   WHERE owner_trigger.tgrelid=to_regclass('control_plane.'||required.relation_name)
    AND owner_trigger.tgname=required.trigger_name AND owner_trigger.tgenabled='O'))
 AND NOT EXISTS(SELECT 1 FROM required_project_triggers required
  WHERE NOT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger immutable_trigger
   WHERE immutable_trigger.tgrelid=to_regclass('control_plane.'||required.relation_name)
    AND immutable_trigger.tgname=required.immutable_name AND immutable_trigger.tgenabled='O'
    AND immutable_trigger.tgtype=19 AND immutable_trigger.tgnargs=0 AND immutable_trigger.tgqual IS NULL
    AND immutable_trigger.tgfoid=to_regprocedure('control_plane.reject_integration_immutable_update()'))
  OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_trigger purge_trigger
   WHERE purge_trigger.tgrelid=to_regclass('control_plane.'||required.relation_name)
    AND purge_trigger.tgname=required.purge_name AND purge_trigger.tgenabled='O'
    AND purge_trigger.tgtype=11 AND purge_trigger.tgnargs=1 AND purge_trigger.tgqual IS NULL
    AND purge_trigger.tgfoid=to_regprocedure('control_plane.guard_ssh_project_receipt_delete()')
    AND purge_trigger.tgargs=convert_to(required.key_name,'UTF8')||decode('00','hex')))
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_proc owner_function
  JOIN pg_catalog.pg_namespace namespace ON namespace.oid=owner_function.pronamespace
  WHERE namespace.nspname='control_plane' AND owner_function.proname='ssh_invocation_identity_current'
   AND owner_function.pronargs=1 AND owner_function.proargtypes[0]=to_regtype('control_plane.ssh_invocation_identity_bindings')::oid
   AND owner_function.prorettype='boolean'::regtype
   AND owner_function.provolatile='v' AND has_function_privilege(session_user,owner_function.oid,'EXECUTE'));
