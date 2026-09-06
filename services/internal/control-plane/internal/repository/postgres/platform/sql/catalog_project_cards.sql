-- name: catalog_project_cards :many
WITH requested AS MATERIALIZED (
    SELECT p.id,p.ref,p.organization_id
    FROM control_plane.projects p
    WHERE p.organization_id=@organization_id::uuid AND p.ref=ANY(@refs::text[])
      AND (@authority_project='' OR p.id=NULLIF(@authority_project,'')::uuid)
), visible AS MATERIALIZED (
    SELECT target.*
    FROM control_plane.catalog_access_targets target
    JOIN requested p ON p.organization_id=target.organization_id AND p.id=target.project_id
    WHERE target.kind IN ('AGENT','WORKFLOW','RUN')
      AND control_plane.catalog_resource_visible(target.organization_id,@actor_id::uuid,
          CASE target.kind WHEN 'AGENT' THEN 'agent.view' WHEN 'WORKFLOW' THEN 'workflow.view' ELSE 'run.view' END,
          target.kind,target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp())
), visible_runs AS MATERIALIZED (
    SELECT r.* FROM control_plane.runs r JOIN visible target ON target.kind='RUN' AND target.id=r.id
    WHERE r.id=r.root_run_id
), connections AS MATERIALIZED (
    SELECT DISTINCT target.project_id,c.id,c.enabled,c.state,g.enabled AS grant_enabled,g.updated_at AS grant_updated_at,c.updated_at
    FROM control_plane.integration_grants g
    JOIN visible target ON target.organization_id=g.organization_id AND target.kind=g.target_kind AND target.ref=g.target_ref
    JOIN control_plane.integration_connections c ON c.id=g.connection_id AND c.organization_id=g.organization_id
    WHERE c.lifecycle_state='ACTIVE'
      AND control_plane.catalog_resource_visible(c.organization_id,@actor_id::uuid,'integration.view',
          'INTEGRATION',c.id,NULL,c.created_by,'{}'::jsonb,statement_timestamp())
), activity AS (
    SELECT r.project_id,r.updated_at AS at FROM visible_runs r
    UNION ALL
    SELECT target.project_id,a.updated_at FROM visible target JOIN control_plane.agents a ON target.kind='AGENT' AND target.id=a.id
    UNION ALL
    SELECT target.project_id,w.updated_at FROM visible target JOIN control_plane.workflows w ON target.kind='WORKFLOW' AND target.id=w.id
    UNION ALL
    SELECT project_id,greatest(updated_at,grant_updated_at) FROM connections
    UNION ALL
    SELECT r.project_id,COALESCE(g.resolved_at,g.created_at) FROM control_plane.owner_gates g JOIN visible_runs r ON r.id=g.root_run_id
)
SELECT p.ref,
       (SELECT count(*)::integer FROM visible v JOIN control_plane.agents a ON v.kind='AGENT' AND v.id=a.id WHERE v.project_id=p.id AND a.state<>'ARCHIVED'),
       (SELECT count(*)::integer FROM visible v JOIN control_plane.workflows w ON v.kind='WORKFLOW' AND v.id=w.id WHERE v.project_id=p.id AND w.state<>'ARCHIVED'),
       (SELECT count(*)::integer FROM visible_runs r WHERE r.project_id=p.id AND r.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')),
       (SELECT count(*)::integer FROM control_plane.owner_gates g JOIN visible_runs r ON r.id=g.root_run_id WHERE r.project_id=p.id AND g.state='OPEN'),
       (SELECT max(activity.at) FROM activity WHERE activity.project_id=p.id),
       CASE
         WHEN NOT EXISTS(SELECT 1 FROM connections c WHERE c.project_id=p.id) THEN 'NONE'
         WHEN EXISTS(SELECT 1 FROM connections c WHERE c.project_id=p.id
                     AND (NOT c.enabled OR NOT c.grant_enabled OR c.state IN ('NOT_CONNECTED','DEGRADED','DISABLED'))) THEN 'DEGRADED'
         WHEN EXISTS(SELECT 1 FROM connections c WHERE c.project_id=p.id AND c.state<>'CONNECTED') THEN 'UNKNOWN'
         ELSE 'READY'
       END
FROM requested p;
