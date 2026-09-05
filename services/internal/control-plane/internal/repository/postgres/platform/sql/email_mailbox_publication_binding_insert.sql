-- name: email_mailbox_publication_binding_insert :exec
INSERT INTO control_plane.email_mailbox_publication_bindings
    (publication_ref,organization_id,connection_id,connection_version,configuration_set_id,revision_id)
VALUES($1,$2::uuid,$3::uuid,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid);
