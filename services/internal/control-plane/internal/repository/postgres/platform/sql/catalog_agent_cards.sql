-- name: catalog_agent_cards :many
WITH requested AS MATERIALIZED (
    SELECT a.id,a.ref,a.organization_id,a.project_id
    FROM control_plane.agents a
    WHERE a.organization_id=@organization_id::uuid AND a.ref=ANY(@refs::text[])
      AND (@authority_project='' OR a.project_id=NULLIF(@authority_project,'')::uuid)
), visible_runs AS MATERIALIZED (
    SELECT r.id,r.ref,r.updated_at
    FROM control_plane.runs r
    JOIN control_plane.catalog_access_targets target ON target.kind='RUN' AND target.id=r.id
      AND target.organization_id=r.organization_id
    WHERE r.organization_id=@organization_id::uuid
      AND r.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
      AND (@authority_project='' OR r.project_id=NULLIF(@authority_project,'')::uuid)
      AND control_plane.catalog_resource_visible(target.organization_id,@actor_id::uuid,'run.view',
          target.kind,target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp())
)
SELECT requested.ref,COALESCE(activity.ref,'')
FROM requested
LEFT JOIN LATERAL (
    SELECT r.ref
    FROM control_plane.run_nodes node
    JOIN visible_runs r ON r.id=node.run_id
    WHERE node.agent_id=requested.id AND node.organization_id=requested.organization_id
      AND node.type='AGENT_EXECUTION' AND node.state IN ('QUEUED','RUNNING','WAITING')
    ORDER BY r.updated_at DESC,r.ref DESC LIMIT 1
) activity ON true;
