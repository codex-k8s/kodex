-- +goose Up
SET ROLE control_plane_owner;

-- +goose StatementBegin
CREATE FUNCTION control_plane.integration_grant_admission(
    p_tenant uuid, p_actor uuid, p_authority_project uuid,
    p_connection_ref text, p_project_ref text, p_recipient_kind text,
    p_recipient_ref text, p_capability_key text, p_purpose text,
    p_workflow_capabilities text[]
) RETURNS TABLE (
    connection_id uuid, connection_ref text, connection_version bigint,
    definition_key text, definition_version text, definition_digest text,
    project_id uuid, project_ref text, project_name text, project_version bigint,
    recipient_kind text, recipient_ref text, recipient_name text,
    recipient_version bigint, capability_key text, capability_name text, reason text
) LANGUAGE sql STABLE SECURITY INVOKER
SET search_path = pg_catalog, control_plane
AS $$
WITH recipients AS (
    SELECT agent.organization_id, agent.project_id, agent.id, agent.ref,
           agent.name, agent.version, agent.created_by,
           'AGENT'::text AS kind,
           CASE p_purpose WHEN 'GRANT' THEN 'agent.manage' ELSE 'agent.view' END AS permission,
           agent.enabled AND agent.state='READY' AS ready
    FROM control_plane.agents agent
    WHERE agent.organization_id=p_tenant AND agent.state<>'ARCHIVED'
      AND p_recipient_kind IN ('', 'AGENT')
      AND (p_recipient_ref='' OR agent.ref=p_recipient_ref)
    UNION ALL
    SELECT workflow.organization_id, workflow.project_id, workflow.id, workflow.ref,
           workflow.name, workflow.version, workflow.created_by,
           'WORKFLOW', 'workflow.manage', workflow.state IN ('DRAFT','VALID','PUBLISHED')
    FROM control_plane.workflows workflow
    WHERE workflow.organization_id=p_tenant AND workflow.state<>'ARCHIVED'
      AND p_recipient_kind IN ('', 'WORKFLOW') AND p_purpose='GRANT'
      AND (p_recipient_ref='' OR workflow.ref=p_recipient_ref)
), connections AS (
    SELECT connection.*, definition.enabled AS definition_enabled,
           definition.adapter_readiness, definition.adapter_owner, definition.execution_route,
           (definition.credential_secret_key IS NULL OR EXISTS (
               SELECT 1 FROM control_plane.integration_credential_revisions credential
               WHERE credential.id=connection.credential_revision_id
                 AND credential.connection_id=connection.id AND credential.organization_id=p_tenant
                 AND connection.masked_credentials_state='CONFIGURED'
           )) AS credential_ready,
           CASE WHEN connection.definition_version=definition.definition_version
                     AND connection.definition_digest=definition.digest
                THEN definition.capabilities
                ELSE (
                    SELECT revision.content::jsonb #> '{spec,capabilities}'
                    FROM control_plane.managed_configuration_bindings binding
                    JOIN control_plane.managed_configuration_sets configuration
                      ON configuration.id=binding.configuration_set_id
                     AND configuration.organization_id=binding.organization_id
                    JOIN control_plane.managed_configuration_revisions revision
                      ON revision.id=binding.configuration_revision_id
                     AND revision.organization_id=binding.organization_id
                     AND revision.configuration_set_id=configuration.id
                    WHERE binding.organization_id=p_tenant
                      AND binding.consumer_kind='INTEGRATION_CONNECTION'
                      AND binding.consumer_ref=connection.ref
                      AND binding.configuration_kind='INTEGRATION_DEFINITION'
                      AND configuration.kind='INTEGRATION_DEFINITION'
                      AND revision.state='PUBLISHED' AND revision.content_format='JSON'
                      AND encode(public.digest(convert_to(revision.content,'UTF8'),'sha256'),'hex')=connection.definition_digest
                      AND revision.content::jsonb #>> '{metadata,key}'=connection.definition_key
                      AND revision.content::jsonb #>> '{metadata,version}'=connection.definition_version
                ) END AS actual_capabilities
    FROM control_plane.integration_connections connection
    JOIN control_plane.integration_definitions definition ON definition.stable_key=connection.definition_key
    WHERE connection.organization_id=p_tenant AND connection.lifecycle_state='ACTIVE'
      AND (p_connection_ref='' OR connection.ref=p_connection_ref)
      AND (control_plane.catalog_resource_visible(p_tenant,p_actor,'integration.manage','INTEGRATION',
          connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
        OR (p_purpose='USE' AND control_plane.catalog_resource_visible(p_tenant,p_actor,'integration.view','INTEGRATION',
          connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())))
)
SELECT connection.id, connection.ref, connection.version,
       connection.definition_key, connection.definition_version, connection.definition_digest,
       project.id, project.ref, project.name, project.version,
       recipient.kind, recipient.ref, recipient.name, recipient.version,
       capability.value->>'key', capability.value->>'name',
       CASE
         WHEN NOT connection.enabled OR connection.state<>'CONNECTED'
              OR NOT connection.credential_ready
              OR NOT connection.definition_enabled OR connection.adapter_readiness<>'READY'
              OR (connection.adapter_owner,connection.execution_route) NOT IN
                 (('integration-gateway','MANAGED_MCP'),('interaction-gateway','INTERACTION'))
           THEN 'CONNECTION_UNAVAILABLE'
         WHEN NOT recipient.ready THEN 'RECIPIENT_UNAVAILABLE'
         WHEN p_purpose='USE' AND NOT EXISTS (
           SELECT 1 FROM control_plane.integration_grants grant_row
           WHERE grant_row.organization_id=p_tenant AND grant_row.connection_id=connection.id
             AND grant_row.target_kind='AGENT' AND grant_row.target_ref=recipient.ref
             AND grant_row.capability_key=capability.value->>'key' AND grant_row.enabled
             AND grant_row.definition_version=connection.definition_version
             AND grant_row.definition_digest=connection.definition_digest
         ) THEN 'GRANT_UNAVAILABLE'
         WHEN p_purpose='USE' AND p_workflow_capabilities IS NOT NULL
           AND NOT (capability.value->>'key'=ANY(p_workflow_capabilities)) THEN 'WORKFLOW_EXCLUDED'
         ELSE 'READY'
       END
FROM recipients recipient
JOIN control_plane.projects project ON project.id=recipient.project_id
 AND project.organization_id=p_tenant AND project.lifecycle='ACTIVE'
CROSS JOIN connections connection
CROSS JOIN LATERAL jsonb_array_elements(connection.actual_capabilities) capability(value)
WHERE p_purpose IN ('GRANT','USE')
  AND (p_project_ref='' OR project.ref=p_project_ref)
  AND (p_authority_project IS NULL OR project.id=p_authority_project)
  AND (p_capability_key='' OR capability.value->>'key'=p_capability_key)
  AND control_plane.catalog_resource_visible(p_tenant,p_actor,recipient.permission,recipient.kind,
      recipient.id,project.id,recipient.created_by,
      jsonb_build_object('PROJECT',project.id::text),transaction_timestamp())
  AND (p_purpose='GRANT' OR (
      capability.value->>'operation' NOT IN ('mattermost.inbound','mattermost.gate_decisions')
      AND capability.value->>'risk' IN ('READ','WRITE','SENSITIVE','DESTRUCTIVE')
      AND control_plane.catalog_resource_visible(p_tenant,p_actor,
          CASE capability.value->>'risk' WHEN 'READ' THEN 'integration.view' ELSE 'integration.manage' END,
          'INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
  ));
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[]) TO control_plane_runtime;
RESET ROLE;
