-- Schema-only fixture: существующая Definition служит FK, не утверждает
-- готовность KUBERNETES package, RPC, owner authorization либо target transport.
WITH connection AS (
 INSERT INTO control_plane.integration_connections(
  id,ref,organization_id,definition_key,name,state,enabled,credential_materialization_ref,
  masked_credentials_state,public_configuration,created_by,definition_version,definition_digest,credential_policy)
 SELECT @connection_id::uuid,'conn_setup_ledger',@organization_id::uuid,stable_key,'Synthetic setup ledger','DISABLED',false,NULL,
  'NOT_CONFIGURED','{}',@actor_id::uuid,definition_version,digest,'INITIATOR_REQUIRED'
 FROM control_plane.integration_definitions WHERE stable_key='gitlab'
 RETURNING id,organization_id
), binding AS (
 INSERT INTO control_plane.integration_personal_credentials(
  ref,organization_id,connection_id,subject_id,credential_kind,broker_purpose)
 SELECT 'ipc_setup_ledger',organization_id,id,@actor_id::uuid,'BROKER_IDENTITY','KUBERNETES_TARGET' FROM connection
 RETURNING id,organization_id,connection_id,subject_id
), identity AS (
 INSERT INTO control_plane.integration_broker_identity_revisions(
  id,ref,organization_id,connection_id,binding_id,subject_id,credential_policy,purpose,target_ref,revision)
 SELECT @identity_id::uuid,'bki_setup_ledger',organization_id,connection_id,id,subject_id,'INITIATOR_REQUIRED','KUBERNETES_TARGET',@target_ref,1
 FROM binding RETURNING id,organization_id,connection_id
), stamp AS (SELECT @created_at::timestamptz AS at)
INSERT INTO control_plane.kubernetes_identity_setups(
 ref,organization_id,connection_id,actor_id,identity_revision_id,registry_revision,target_ref,target_revision,
 expected_connection_version,expected_binding_version,expected_binding_generation,definition_digest,configuration_digest,
 begin_idempotency_key,intent_sha256,permit_sha256,snapshot_sha256,content_sha256,content_bytes,created_at,expires_at)
SELECT 'ksu_setup_ledger_fixture',organization_id,connection_id,@actor_id::uuid,id,1,@target_ref,1,1,1,1,
 @definition_digest,@configuration_digest,'setup-ledger-fixture',@intent_digest,repeat('d',64),@snapshot_digest,repeat('f',64),12,
 stamp.at,stamp.at+interval '90 seconds' FROM identity CROSS JOIN stamp
RETURNING id::text;
