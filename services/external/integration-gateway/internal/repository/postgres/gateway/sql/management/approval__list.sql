SELECT payload, version
  FROM integration_gateway.approvals
 WHERE tenant_id = @tenant_id AND project_id = @project_id
   AND approval_id > @after_id
   AND (cardinality(@states::text[]) = 0 OR status = ANY(@states::text[]))
 ORDER BY approval_id
 LIMIT @page_limit
