-- name: email_invocation__pin :exec
INSERT INTO control_plane.email_invocation_mailbox_bindings(
  invocation_id,organization_id,connection_id,root_run_id,initiator_id,
  owner_subject_id,owner_generation,mailbox_ref,mailbox_revision,mailbox_source_digest)
VALUES(@invocation_id::uuid,@organization_id::uuid,@connection_id::uuid,@root_run_id::uuid,@initiator_id::uuid,
  @owner_subject_id::uuid,@owner_generation,@mailbox_ref,@mailbox_revision,@mailbox_source_digest);
