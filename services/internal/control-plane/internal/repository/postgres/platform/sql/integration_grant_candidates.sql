-- name: integration_grant_candidates :one
WITH admission_source AS MATERIALIZED (
    SELECT admission.*, connection.name AS connection_name, definition.name AS provider_name,
           CASE @stage WHEN 'CONNECTION' THEN 'integration.view'
                       WHEN 'PROJECT' THEN 'project.view'
                       WHEN 'RECIPIENT' THEN CASE recipient.kind WHEN 'AGENT' THEN 'agent.view' ELSE 'workflow.view' END
                       ELSE '' END AS visibility_permission,
           CASE @stage WHEN 'CONNECTION' THEN 'INTEGRATION'
                       WHEN 'PROJECT' THEN 'PROJECT' ELSE recipient.kind END AS visibility_kind,
           CASE @stage WHEN 'CONNECTION' THEN connection.id
                       WHEN 'PROJECT' THEN project.id ELSE recipient.id END AS visibility_id,
           CASE @stage WHEN 'CONNECTION' THEN NULL::uuid
                       WHEN 'PROJECT' THEN project.id ELSE recipient.project_id END AS visibility_project_id,
           CASE @stage WHEN 'CONNECTION' THEN connection.created_by
                       WHEN 'PROJECT' THEN project.created_by ELSE recipient.owner_id END AS visibility_owner_id,
           CASE WHEN @stage IN ('CONNECTION','PROJECT') THEN '{}'::jsonb
                ELSE recipient.related_ids END AS visibility_related_ids
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
    -- Поставленный OpenAPI-шаблон не исполняется без опубликованной owner-ревизии.
    -- Исключаем его до подсчёта и пагинации, иначе одна такая запись ломает
    -- весь каталог при последующей проверке integrationPackage.
    WHERE NOT (admission.definition_key='openapi-mcp'
      AND admission.definition_version=definition.definition_version
      AND admission.definition_digest=definition.digest)
      AND (@purpose='GRANT' OR admission.reason='READY')
), visibility_targets AS MATERIALIZED (
    -- Одна eligibility-проверка exact resource, а не каждой capability row.
    -- Все аргументы предиката сохраняются внутри того же owner snapshot.
    SELECT DISTINCT visibility_permission,visibility_kind,visibility_id,
           visibility_project_id,visibility_owner_id,visibility_related_ids
    FROM admission_source
), visible_targets AS MATERIALIZED (
    SELECT visibility_permission,visibility_kind,visibility_id,
           visibility_project_id,visibility_owner_id,visibility_related_ids
    FROM visibility_targets
    WHERE CASE WHEN visibility_permission='' THEN true
          ELSE control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,
              visibility_permission,visibility_kind,visibility_id,visibility_project_id,
              visibility_owner_id,visibility_related_ids,transaction_timestamp()) END
), admitted AS MATERIALIZED (
    SELECT source.connection_ref,source.connection_version,source.definition_key,
           source.definition_version,source.definition_digest,source.provider_name,
           source.project_ref,source.project_name,source.project_version,
           source.recipient_kind,source.recipient_ref,source.recipient_name,
           source.recipient_version,source.capability_key,source.capability_name,
           source.reason,source.connection_name
    FROM admission_source source
    JOIN visible_targets visible
      ON (source.visibility_permission,source.visibility_kind,source.visibility_id,
          source.visibility_project_id,source.visibility_owner_id,source.visibility_related_ids)
         IS NOT DISTINCT FROM
         (visible.visibility_permission,visible.visibility_kind,visible.visibility_id,
          visible.visibility_project_id,visible.visibility_owner_id,visible.visibility_related_ids)
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
