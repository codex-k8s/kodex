SELECT m.safe_projection
FROM control_plane.email_mailbox_projections m
JOIN control_plane.email_configuration_watermark w ON w.singleton
JOIN control_plane.integration_connections c ON c.id=m.connection_id AND c.organization_id=m.organization_id
WHERE m.organization_id=$1::uuid AND m.ref=$2 AND ($3::bigint=0 OR m.revision=$3)
  AND m.enabled AND NOT m.removed AND m.document_revision=w.revision
  AND w.revision=$4 AND w.digest=$5
  AND c.enabled AND c.definition_key='email'
  AND c.public_configuration->>'mailbox_id'=m.ref
  AND c.public_configuration->>'from_address'=m.safe_projection->>'sender'
FOR SHARE OF m,w,c;
