-- name: email_credential__receipt_current :one
SELECT EXISTS(SELECT 1 FROM control_plane.email_credential_descriptors descriptor
  JOIN control_plane.integration_connections connection ON connection.id=descriptor.connection_id
    AND connection.organization_id=descriptor.organization_id
  WHERE descriptor.organization_id=@organization_id::uuid AND connection.ref=@connection_ref
    AND connection.mailbox_owner_state='ACTIVE' AND connection.mailbox_owner_generation=@owner_generation
    AND descriptor.name=@name AND descriptor.generation=@generation AND descriptor.kind=@kind
    AND descriptor.mailbox_owner_generation=connection.mailbox_owner_generation
    AND descriptor.created_by=connection.mailbox_owner_configured_by);
