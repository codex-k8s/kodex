-- name: queries_search_select_eligible_resources :many
WITH eligible_projects AS (
    SELECT project.id, project.ref
    FROM control_plane.projects AS project
    WHERE project.organization_id = @organization_id::uuid
      AND project.lifecycle <> 'ARCHIVED'
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND (
          @actor_platform_role IN ('OWNER', 'ADMINISTRATOR')
          OR EXISTS (
              SELECT 1 FROM control_plane.memberships AS membership
              WHERE membership.project_id = project.id
                AND membership.subject_id = @actor_id::uuid
                AND membership.active AND 'VIEW' = ANY(membership.permissions)
          )
      )
), matches AS (
    SELECT 'PROJECT'::text AS kind, project.ref, project.ref AS project_ref,
           project.name AS title, project.purpose AS subtitle, project.lifecycle AS state,
           project.updated_at,
           CASE WHEN lower(project.name) = lower(@query) THEN 0
                WHEN project.name ILIKE @query || '%' THEN 1 ELSE 2 END AS relevance
    FROM control_plane.projects AS project JOIN eligible_projects AS eligible ON eligible.id = project.id
    WHERE project.name ILIKE '%' || @query || '%' OR project.purpose ILIKE '%' || @query || '%'
    UNION ALL
    SELECT 'AGENT', agent.ref, project.ref, agent.name, agent.purpose, agent.state, agent.updated_at,
           CASE WHEN lower(agent.name) = lower(@query) THEN 0 WHEN agent.name ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.agents AS agent JOIN eligible_projects AS project ON project.id = agent.project_id
    WHERE agent.system_key IS NULL AND agent.state <> 'ARCHIVED'
      AND (agent.name ILIKE '%' || @query || '%' OR agent.purpose ILIKE '%' || @query || '%')
    UNION ALL
    SELECT 'WORKFLOW', workflow.ref, project.ref, workflow.name, workflow.purpose,
           workflow.state, workflow.updated_at,
           CASE WHEN lower(workflow.name) = lower(@query) THEN 0 WHEN workflow.name ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.workflows AS workflow JOIN eligible_projects AS project ON project.id = workflow.project_id
    WHERE workflow.state <> 'ARCHIVED'
      AND (workflow.name ILIKE '%' || @query || '%' OR workflow.purpose ILIKE '%' || @query || '%')
    UNION ALL
    SELECT 'RUN', run.ref, project.ref, run.title, run.task, run.state, run.updated_at,
           CASE WHEN lower(run.title) = lower(@query) THEN 0 WHEN run.title ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.runs AS run JOIN eligible_projects AS project ON project.id = run.project_id
    WHERE run.title ILIKE '%' || @query || '%' OR run.task ILIKE '%' || @query || '%'
), total AS (SELECT count(*)::bigint AS value FROM matches)
SELECT match.kind, match.ref, match.project_ref, match.title, match.subtitle, match.state,
       match.updated_at, match.relevance, total.value
FROM matches AS match CROSS JOIN total
WHERE @cursor_time::timestamptz IS NULL
   OR match.relevance > @cursor_relevance
   OR (match.relevance = @cursor_relevance AND match.updated_at < @cursor_time::timestamptz)
   OR (match.relevance = @cursor_relevance AND match.updated_at = @cursor_time::timestamptz
       AND (match.kind, match.ref) > (@cursor_kind, @cursor_ref))
ORDER BY match.relevance, match.updated_at DESC, match.kind, match.ref
LIMIT @page_size;
