INSERT INTO control_plane.email_invocation_mailbox_bindings(
 invocation_id,organization_id,connection_id,root_run_id,initiator_id,
 owner_subject_id,owner_generation,mailbox_ref,mailbox_revision,mailbox_source_digest)
SELECT pin.invocation_id,
 CASE WHEN @bad_case='organization' THEN '00000000-0000-4000-8000-000000002168'::uuid ELSE pin.organization_id END,
 CASE WHEN @bad_case='connection' THEN (SELECT id FROM control_plane.integration_connections WHERE ref=@other_connection) ELSE pin.connection_id END,
 CASE WHEN @bad_case='root' THEN '00000000-0000-4000-8000-000000002168'::uuid ELSE pin.root_run_id END,
 CASE WHEN @bad_case='initiator' THEN @other_subject::uuid ELSE pin.initiator_id END,
 CASE WHEN @bad_case='owner' THEN @other_subject::uuid ELSE pin.owner_subject_id END,
 pin.owner_generation+CASE WHEN @bad_case='generation' THEN 1 ELSE 0 END,
 CASE WHEN @bad_case='mailbox' THEN 'other-mailbox' ELSE pin.mailbox_ref END,
 pin.mailbox_revision+CASE WHEN @bad_case='revision' THEN 1 ELSE 0 END,
 CASE WHEN @bad_case='digest' THEN repeat('0',64) ELSE pin.mailbox_source_digest END
FROM control_plane.email_invocation_mailbox_bindings pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id
WHERE invocation.ref=@invocation_ref;
