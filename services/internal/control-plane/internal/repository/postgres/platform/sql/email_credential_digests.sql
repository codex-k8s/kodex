-- name: email_credential_digests :many
SELECT descriptor.name,descriptor.generation,descriptor.kind,descriptor.content_sha256,connection.ref,organization.ref
FROM control_plane.email_credential_descriptors descriptor
JOIN control_plane.integration_connections connection ON connection.id=descriptor.connection_id AND connection.organization_id=descriptor.organization_id
JOIN control_plane.organizations organization ON organization.id=descriptor.organization_id
WHERE descriptor.name||'.'||descriptor.generation=ANY($1::text[]);
