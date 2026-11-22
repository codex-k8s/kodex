-- name: email_invocation__matches :one
SELECT EXISTS(SELECT 1 FROM control_plane.email_invocation_mailbox_bindings binding
WHERE binding.organization_id=@organization_id::uuid AND binding.invocation_id=@invocation_id::uuid
  AND binding.connection_id=@connection_id::uuid AND binding.root_run_id=@root_run_id::uuid
  AND binding.initiator_id=@initiator_id::uuid AND binding.owner_subject_id=@owner_subject_id::uuid
  AND binding.owner_generation=@owner_generation AND binding.mailbox_ref=@mailbox_ref
  AND binding.mailbox_revision=@mailbox_revision AND binding.mailbox_source_digest=@mailbox_source_digest);
