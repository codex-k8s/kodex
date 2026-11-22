-- name: ssh_native_owner_snapshot :one
SELECT md5(jsonb_build_object(
 'connections',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.integration_connections v),
 'bindings',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.integration_personal_credentials v),
 'identities',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.integration_broker_identity_revisions v),
 'setups',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.ssh_identity_setups v),
 'attempts',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.ssh_identity_upload_attempts v),
 'observations',(SELECT jsonb_agg(to_jsonb(v) ORDER BY attempt_id,outcome,receipt_sha256) FROM control_plane.ssh_identity_upload_observations v),
 'material',(SELECT jsonb_agg(to_jsonb(v) ORDER BY identity_revision_id) FROM control_plane.ssh_identity_material_revisions v),
 'rejections',(SELECT jsonb_agg(to_jsonb(v) ORDER BY attempt_id,response_sha256) FROM control_plane.ssh_identity_ack_rejections v),
 'quarantines',(SELECT jsonb_agg(to_jsonb(v) ORDER BY identity_revision_id) FROM control_plane.ssh_identity_quarantines v),
 'audit',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.audit_events v)
)::text);
