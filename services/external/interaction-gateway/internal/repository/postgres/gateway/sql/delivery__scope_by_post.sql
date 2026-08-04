SELECT COALESCE(organization_id::text, ''), COALESCE(project_id::text, '')
FROM interaction_gateway_delivery_scope_by_post($1);
