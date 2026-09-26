-- name: queries_listintegrationdefinitions_select_integration_definitions_category :many
SELECT d.stable_key,d.name,d.description,d.category,d.optional,d.enabled,d.capabilities,d.configuration_schema,
	d.schema_version,d.definition_version,d.origin,d.digest,d.adapter,COALESCE(d.credential_secret_key,''),
	d.adapter_owner,d.execution_route,d.adapter_readiness,d.version,
	connection_summary.connection_count,connection_summary.healthy_connection_count
FROM control_plane.integration_definitions d
CROSS JOIN LATERAL (
	SELECT COUNT(*)::bigint AS connection_count,
		COUNT(*) FILTER (WHERE c.state='CONNECTED')::bigint AS healthy_connection_count
	FROM control_plane.integration_connections c
	WHERE c.organization_id=$1::uuid
	  AND c.definition_key=d.stable_key
	  AND c.lifecycle_state='ACTIVE'
) connection_summary
WHERE ($2 = '' OR d.category = $2)
  AND ($3 = '' OR d.stable_key ILIKE '%' || $3 || '%' OR d.name ILIKE '%' || $3 || '%' OR d.description ILIKE '%' || $3 || '%')
  AND ($4 = '' OR d.stable_key > $4)
ORDER BY d.stable_key
LIMIT $5
