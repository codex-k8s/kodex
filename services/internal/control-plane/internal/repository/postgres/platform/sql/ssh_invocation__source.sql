-- name: ssh_invocation__source :one
SELECT identity.id::text,identity.ref,identity.revision,identity.target_ref,pin.transport_mode,pin.subject_digest,pin.valid_until,
 material.namespace,material.secret_name,material.secret_uid,material.secret_resource_version,material.content_sha256,material.authentication_sha256
FROM control_plane.integration_invocations invocation
JOIN control_plane.ssh_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=pin.identity_revision_id AND identity.organization_id=pin.organization_id
JOIN control_plane.ssh_identity_material_revisions material ON material.identity_revision_id=identity.id AND material.organization_id=identity.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND control_plane.ssh_invocation_identity_current(pin);
