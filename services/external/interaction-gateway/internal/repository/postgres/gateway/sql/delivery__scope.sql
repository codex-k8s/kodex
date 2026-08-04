SELECT COALESCE(organization_id::text, ''), COALESCE(project_id::text, '')
FROM interaction_gateway_delivery_scope($1::uuid);
