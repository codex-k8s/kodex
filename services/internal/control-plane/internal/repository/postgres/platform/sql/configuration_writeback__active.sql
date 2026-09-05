-- name: configuration_writeback__active :many
SELECT work.ref FROM control_plane.managed_configuration_writebacks work
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=work.configuration_set_id AND configuration.organization_id=work.organization_id
JOIN control_plane.integration_connections connection ON connection.id=work.connection_id AND connection.organization_id=work.organization_id
WHERE work.organization_id=$1::uuid AND work.state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')
  AND (($2<>'' AND configuration.ref=$2) OR ($3<>'' AND connection.ref=$3))
ORDER BY work.ref;
