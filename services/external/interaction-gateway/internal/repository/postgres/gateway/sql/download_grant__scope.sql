SELECT organization_id::text, project_id::text
FROM interaction_gateway_download_grant_scope($1::uuid);
