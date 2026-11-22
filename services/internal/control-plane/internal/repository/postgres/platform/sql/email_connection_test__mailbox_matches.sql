-- name: email_connection_test__mailbox_matches :one
SELECT EXISTS(SELECT 1 FROM control_plane.integration_connection_tests test
  WHERE test.organization_id=@organization_id::uuid AND test.id=@test_id::uuid
    AND control_plane.email_connection_test_mailbox_current(test.id)
    AND test.input_snapshot->>'mailboxRef'=@mailbox_ref
    AND test.input_snapshot->>'mailboxRevision'=@mailbox_revision::bigint::text
    AND test.input_snapshot->>'mailboxSourceDigest'=@mailbox_source_digest
    AND test.input_snapshot->>'mailboxOwnerGeneration'=@owner_generation::bigint::text);
