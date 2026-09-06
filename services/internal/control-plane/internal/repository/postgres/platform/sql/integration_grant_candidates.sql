-- name: integration_grant_candidates :one
WITH admitted AS MATERIALIZED (
    SELECT admission.*, connection.name AS connection_name, definition.name AS provider_name
    FROM control_plane.integration_grant_admission(
      @organization_id::uuid,@actor_id::uuid,NULLIF(@authority_project_id,'')::uuid,
      @connection_ref,@project_ref,@recipient_kind,@recipient_ref,@capability_key,
      @purpose,@workflow_capabilities::text[]
    ) admission
    JOIN control_plane.integration_connections connection ON connection.id=admission.connection_id
    JOIN control_plane.integration_definitions definition ON definition.stable_key=admission.definition_key
    JOIN control_plane.projects project ON project.id=admission.project_id
    JOIN control_plane.catalog_access_targets recipient
      ON recipient.organization_id=@organization_id::uuid AND recipient.kind=admission.recipient_kind
     AND recipient.ref=admission.recipient_ref AND recipient.project_id=project.id
    WHERE (@purpose='GRANT' OR admission.reason='READY')
      AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,
          'integration.view','INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
      AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,
          'project.view','PROJECT',project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
      AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,
          CASE recipient.kind WHEN 'AGENT' THEN 'agent.view' ELSE 'workflow.view' END,
          recipient.kind,recipient.id,recipient.project_id,recipient.owner_id,recipient.related_ids,transaction_timestamp())
), projected AS (
    SELECT CASE @stage WHEN 'CONNECTION' THEN connection_ref
                      WHEN 'PROJECT' THEN project_ref
                      WHEN 'RECIPIENT' THEN recipient_ref
                      WHEN 'CAPABILITY' THEN capability_key END AS ref,
           CASE @stage WHEN 'CONNECTION' THEN connection_name
                      WHEN 'PROJECT' THEN project_name
                      WHEN 'RECIPIENT' THEN recipient_name
                      WHEN 'CAPABILITY' THEN capability_name END AS name,
           CASE WHEN @stage='CONNECTION' THEN connection_ref ELSE @connection_ref END AS connection_ref,
           connection_version,definition_key,definition_version,definition_digest,provider_name,
           CASE WHEN @stage<>'CONNECTION' THEN project_ref ELSE @project_ref END AS project_ref,
           CASE WHEN @stage<>'CONNECTION' OR @purpose='USE' THEN project_version ELSE 0 END AS project_version,
           CASE WHEN @stage IN ('RECIPIENT','CAPABILITY') THEN recipient_kind ELSE @recipient_kind END AS recipient_kind,
           CASE WHEN @stage IN ('RECIPIENT','CAPABILITY') THEN recipient_ref ELSE @recipient_ref END AS recipient_ref,
           CASE WHEN @stage IN ('RECIPIENT','CAPABILITY') OR @purpose='USE' THEN recipient_version ELSE 0 END AS recipient_version,
           CASE WHEN @stage='CAPABILITY' THEN capability_key ELSE @capability_key END AS capability_key,
           reason
    FROM admitted
), candidates AS MATERIALIZED (
    SELECT ref,name,connection_ref,connection_version,definition_key,definition_version,definition_digest,
           provider_name,project_ref,project_version,recipient_kind,recipient_ref,recipient_version,capability_key,
           CASE WHEN bool_or(reason='READY') THEN 'READY'
                WHEN bool_or(reason='CONNECTION_UNAVAILABLE') THEN 'CONNECTION_UNAVAILABLE'
                ELSE 'RECIPIENT_UNAVAILABLE' END AS reason
    FROM projected
    WHERE ref IS NOT NULL AND (@query='' OR strpos(lower(name || ' ' || ref),lower(@query))>0)
    GROUP BY ref,name,connection_ref,connection_version,definition_key,definition_version,definition_digest,
             provider_name,project_ref,project_version,recipient_kind,recipient_ref,recipient_version,capability_key
), page AS (
    SELECT * FROM candidates WHERE ref>@after_ref ORDER BY ref LIMIT @page_limit
)
SELECT (SELECT count(*) FROM candidates),
       COALESCE((SELECT jsonb_agg(to_jsonb(page) ORDER BY ref) FROM page),'[]'::jsonb)
