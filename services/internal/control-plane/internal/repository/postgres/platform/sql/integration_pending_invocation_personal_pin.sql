-- name: integration_pending_invocation_personal_pin :one
SELECT EXISTS (
 SELECT 1 FROM control_plane.integration_personal_invocation_credentials pin
 JOIN control_plane.integration_invocations i ON i.id=pin.invocation_id AND i.organization_id=pin.organization_id
 JOIN control_plane.integration_personal_credential_revisions revision
  ON revision.id=pin.credential_revision_id AND revision.binding_id=pin.binding_id
  AND revision.organization_id=pin.organization_id
 WHERE pin.organization_id=@organization_id::uuid AND i.ref=@invocation_ref
  AND pin.binding_id=@binding_id::uuid AND pin.binding_generation=@binding_generation::bigint
  AND revision.ref=@credential_ref AND pin.configuration_digest=@configuration_digest
  AND pin.resource_permission_profile=@resource_permission_profile
);
