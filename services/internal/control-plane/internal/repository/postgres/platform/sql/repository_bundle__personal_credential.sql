-- name: repository_bundle__personal_credential :one
SELECT revision.id::text
FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id AND invocation.organization_id=pin.organization_id
JOIN control_plane.integration_personal_credential_revisions revision ON revision.id=pin.credential_revision_id AND revision.organization_id=pin.organization_id AND revision.binding_id=pin.binding_id
WHERE pin.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref AND revision.ref=@credential_ref
