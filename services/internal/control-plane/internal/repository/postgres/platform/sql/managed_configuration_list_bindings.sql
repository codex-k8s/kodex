-- name: managed_configuration_list_bindings :many
WITH binding_snapshot AS MATERIALIZED (
    SELECT binding.consumer_kind, binding.consumer_ref, revision.ref AS revision_ref, binding.version,
           configuration.ref AS configuration_ref,
           binding.consumer_kind || ':' || binding.consumer_ref AS cursor_ref
    FROM control_plane.managed_configuration_bindings binding
    JOIN control_plane.managed_configuration_sets configuration ON configuration.id=binding.configuration_set_id
    JOIN control_plane.managed_configuration_revisions revision ON revision.id=binding.configuration_revision_id
    JOIN control_plane.managed_configuration_sets target
      ON target.organization_id = configuration.organization_id AND target.ref = @configuration_ref
     AND target.kind = configuration.kind
     AND target.project_id IS NOT DISTINCT FROM configuration.project_id
    WHERE configuration.organization_id=@organization_id::uuid
      AND binding.configuration_kind=configuration.kind
), bindings AS MATERIALIZED (
    SELECT * FROM binding_snapshot WHERE configuration_ref = @configuration_ref
), targets AS (
    SELECT organization_id,kind,ref,id,project_id,owner_id,related_ids FROM control_plane.catalog_access_targets
    UNION ALL
    SELECT e.organization_id,'RUNTIME_ENVIRONMENT',e.ref,e.id,e.project_id,p.created_by,
           jsonb_build_object('PROJECT',e.project_id::text)
    FROM control_plane.runtime_environment_sets e
    JOIN control_plane.projects p ON p.id=e.project_id AND p.lifecycle='ACTIVE'
    WHERE e.state='ACTIVE'
    UNION ALL
    SELECT c.organization_id,'INTEGRATION_CONNECTION',c.ref,c.id,NULL::uuid,c.created_by,'{}'::jsonb
    FROM control_plane.integration_connections c WHERE c.lifecycle_state='ACTIVE'
), eligible AS MATERIALIZED (
    SELECT b.* FROM bindings b
    LEFT JOIN targets t ON t.organization_id=@organization_id::uuid AND t.kind=b.consumer_kind AND t.ref=b.consumer_ref
    WHERE (@query='' OR strpos(lower(b.consumer_ref || ' ' || b.consumer_kind),lower(@query))>0)
      AND ((b.consumer_kind='STT_SERVICE' AND b.consumer_ref='stt-tts-service' AND @organization_managed::boolean)
        OR (t.id IS NOT NULL AND (@authority_project='' OR t.project_id=NULLIF(@authority_project,'')::uuid)
          AND control_plane.catalog_resource_visible(t.organization_id,@actor_id::uuid,
            CASE t.kind WHEN 'AGENT' THEN 'agent.manage' WHEN 'WORKFLOW' THEN 'workflow.manage'
              WHEN 'SCHEDULE' THEN 'schedule.manage' WHEN 'RUNTIME_ENVIRONMENT' THEN 'project.manage'
              WHEN 'INTEGRATION_CONNECTION' THEN 'integration.manage' ELSE '' END,
            CASE t.kind WHEN 'RUNTIME_ENVIRONMENT' THEN 'PROJECT' WHEN 'INTEGRATION_CONNECTION' THEN 'INTEGRATION' ELSE t.kind END,
            CASE t.kind WHEN 'RUNTIME_ENVIRONMENT' THEN t.project_id ELSE t.id END,
            t.project_id,t.owner_id,t.related_ids,@evaluated_at)))
), page AS (
    SELECT * FROM eligible WHERE cursor_ref COLLATE "C">@cursor_ref COLLATE "C"
    ORDER BY cursor_ref COLLATE "C" LIMIT @page_size
), commitment AS (
    SELECT encode(sha256(convert_to(@configuration_ref,'UTF8') || decode('00','hex') || convert_to(@revision_ref,'UTF8') ||
        COALESCE(string_agg(decode('00','hex') || convert_to(consumer_kind,'UTF8') || decode('00','hex') ||
          convert_to(consumer_ref,'UTF8') || decode('00','hex') || convert_to(revision_ref,'UTF8') || decode('00','hex') ||
          convert_to(version::text,'UTF8'),''::bytea ORDER BY consumer_kind COLLATE "C",consumer_ref COLLATE "C"),''::bytea)), 'hex') AS digest
    FROM binding_snapshot
)
SELECT COALESCE(page.consumer_kind,''),COALESCE(page.consumer_ref,''),COALESCE(page.revision_ref,''),COALESCE(page.version,0),
       totals.total,commitment.digest
FROM (SELECT count(*) AS total FROM eligible) totals CROSS JOIN commitment LEFT JOIN page ON true
ORDER BY page.cursor_ref COLLATE "C";
