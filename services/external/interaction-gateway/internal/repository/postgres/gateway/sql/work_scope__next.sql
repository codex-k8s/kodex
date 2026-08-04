SELECT COALESCE(organization_id::text, ''), COALESCE(project_id::text, '')
FROM interaction_gateway_next_work_scope($1);
