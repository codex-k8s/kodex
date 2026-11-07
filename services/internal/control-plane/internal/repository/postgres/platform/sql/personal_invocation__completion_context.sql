-- name: personal_invocation__completion_context :one
SELECT connection.ref,COALESCE(pin.binding_id::text,''),
 COALESCE(pin.binding_generation,0),COALESCE(revision.ref,'')
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
 AND connection.organization_id=invocation.organization_id
LEFT JOIN control_plane.integration_personal_invocation_credentials pin
 ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
LEFT JOIN control_plane.integration_personal_credential_revisions revision
 ON revision.id=pin.credential_revision_id AND revision.binding_id=pin.binding_id
 AND revision.organization_id=pin.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref;
