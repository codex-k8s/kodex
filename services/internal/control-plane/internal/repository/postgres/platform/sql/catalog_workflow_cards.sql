-- name: catalog_workflow_cards :many
WITH requested AS MATERIALIZED (
    SELECT w.id,w.ref,w.organization_id,w.project_id,
           CASE WHEN w.published_version>0 THEN w.published_spec ELSE w.draft_spec END AS spec
    FROM control_plane.workflows w
    WHERE w.organization_id=@organization_id::uuid AND w.ref=ANY(@refs::text[])
      AND (@authority_project='' OR w.project_id=NULLIF(@authority_project,'')::uuid)
), visible_runs AS MATERIALIZED (
    SELECT r.*
    FROM control_plane.runs r
    JOIN requested w ON w.organization_id=r.organization_id AND r.target_type='WORKFLOW' AND r.target_ref=w.ref
    JOIN control_plane.catalog_access_targets target ON target.kind='RUN' AND target.id=r.id
      AND target.organization_id=r.organization_id
    WHERE r.id=r.root_run_id
      AND control_plane.catalog_resource_visible(target.organization_id,@actor_id::uuid,'run.view',
          target.kind,target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp())
)
SELECT w.ref,
       (SELECT count(*)::integer FROM jsonb_array_elements(COALESCE(NULLIF(w.spec->'Steps','null'::jsonb),'[]'::jsonb))) AS stage_count,
       (SELECT count(DISTINCT a.id)::integer
        FROM (SELECT step->>'AgentRef' AS ref
              FROM jsonb_array_elements(COALESCE(NULLIF(w.spec->'Steps','null'::jsonb),'[]'::jsonb)) step
              UNION SELECT w.spec->>'CoordinatorAgentRef') participant
        JOIN control_plane.catalog_access_targets a ON a.organization_id=w.organization_id
          AND a.kind='AGENT' AND a.ref=participant.ref
        WHERE control_plane.catalog_resource_visible(a.organization_id,@actor_id::uuid,'agent.view',
          a.kind,a.id,a.project_id,a.owner_id,a.related_ids,statement_timestamp())) AS unique_agent_count,
       (SELECT count(DISTINCT step->>'ParallelGroup')::integer
        FROM jsonb_array_elements(COALESCE(NULLIF(w.spec->'Steps','null'::jsonb),'[]'::jsonb)) step
        WHERE (step->>'Parallel')::boolean AND (step->>'ParallelGroup')::integer>0) AS parallel_group_count,
       EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(NULLIF(w.spec->'Steps','null'::jsonb),'[]'::jsonb)) step
              WHERE (step->>'HumanGateAfter')::boolean),
       (SELECT count(*)::integer FROM visible_runs r WHERE r.target_ref=w.ref
        AND r.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')),
       (SELECT count(*)::integer FROM control_plane.owner_gates gate
        JOIN visible_runs r ON r.id=gate.root_run_id AND r.organization_id=gate.organization_id
        WHERE r.target_ref=w.ref AND gate.state='OPEN'),
       (SELECT max(r.updated_at) FROM visible_runs r WHERE r.target_ref=w.ref)
FROM requested w;
