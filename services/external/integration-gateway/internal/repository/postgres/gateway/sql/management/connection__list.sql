SELECT payload
  FROM integration_gateway.managed_provider_connections
 WHERE tenant_id = @tenant_id AND project_id = @project_id
   AND connection_id > @after_id
   AND (cardinality(@states::text[]) = 0 OR status = ANY(@states::text[]))
 ORDER BY connection_id
 LIMIT @page_limit
