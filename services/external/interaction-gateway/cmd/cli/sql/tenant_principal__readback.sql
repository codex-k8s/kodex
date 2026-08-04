SELECT authority.organization_id::text, tenant.project_id::text
FROM interaction_gateway_runtime_principal_authorities authority
JOIN interaction_gateway_runtime_principal_tenants tenant USING (principal_name, generation, organization_id)
JOIN interaction_gateway_runtime_principals principal USING (principal_name, generation)
JOIN interaction_gateway_runtime_credential_fence fence ON fence.singleton AND fence.served_generation = generation
WHERE authority.generation = $1 AND principal.status = 'CURRENT' ORDER BY tenant.project_id;
