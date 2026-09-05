-- name: email_mailbox_binding_apply :exec
INSERT INTO control_plane.email_mailbox_configuration_bindings(organization_id,connection_id,configuration_set_id,revision_id)
VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid)
ON CONFLICT(connection_id) DO UPDATE SET configuration_set_id=EXCLUDED.configuration_set_id,revision_id=EXCLUDED.revision_id,
    version=control_plane.email_mailbox_configuration_bindings.version+1,updated_at=clock_timestamp();
