-- name: queries_search_select_eligible_resources :many
WITH matches AS (
    SELECT 'PROJECT'::text AS kind, project.ref, project.ref AS project_ref,
           project.name AS title, project.purpose AS subtitle, project.lifecycle AS state,
           project.updated_at, project.created_at AS order_time,
           CASE WHEN lower(project.name) = lower(@query) THEN 0
                WHEN project.name ILIKE @query || '%' THEN 1 ELSE 2 END AS relevance
    FROM control_plane.projects AS project
    WHERE project.organization_id = @organization_id::uuid
      AND project.lifecycle <> 'ARCHIVED'
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND (project.name ILIKE '%' || @query || '%' OR project.purpose ILIKE '%' || @query || '%')
    UNION ALL
    SELECT 'AGENT', agent.ref, project.ref, agent.name, agent.purpose, agent.state,
           agent.updated_at, agent.created_at,
           CASE WHEN lower(agent.name) = lower(@query) THEN 0 WHEN agent.name ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.agents AS agent
    JOIN control_plane.projects AS project ON project.id = agent.project_id
    WHERE agent.organization_id = @organization_id::uuid
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND agent.system_key IS NULL AND agent.state <> 'ARCHIVED'
      AND (agent.name ILIKE '%' || @query || '%' OR agent.purpose ILIKE '%' || @query || '%')
    UNION ALL
    SELECT 'WORKFLOW', workflow.ref, project.ref, workflow.name, workflow.purpose,
           workflow.state, workflow.updated_at, workflow.created_at,
           CASE WHEN lower(workflow.name) = lower(@query) THEN 0 WHEN workflow.name ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.workflows AS workflow
    JOIN control_plane.projects AS project ON project.id = workflow.project_id
    WHERE workflow.organization_id = @organization_id::uuid
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND workflow.state <> 'ARCHIVED'
      AND (workflow.name ILIKE '%' || @query || '%' OR workflow.purpose ILIKE '%' || @query || '%')
    UNION ALL
    SELECT 'RUN', run.ref, project.ref, run.title, run.task, run.state,
           run.updated_at, run.created_at,
           CASE WHEN lower(run.title) = lower(@query) THEN 0 WHEN run.title ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.runs AS run
    JOIN control_plane.projects AS project ON project.id = run.project_id
    WHERE run.organization_id = @organization_id::uuid
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND (run.title ILIKE '%' || @query || '%' OR run.task ILIKE '%' || @query || '%')
    UNION ALL
    SELECT 'ARTIFACT', artifact.ref, project.ref, artifact.file_name, artifact.media_type,
           artifact.lifecycle_state, artifact.created_at, artifact.created_at,
           CASE WHEN lower(artifact.file_name) = lower(@query) THEN 0 WHEN artifact.file_name ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.artifacts AS artifact
    JOIN control_plane.projects AS project ON project.id = artifact.project_id
    WHERE artifact.organization_id = @organization_id::uuid
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND artifact.lifecycle_state = 'ACTIVE'
      AND artifact.file_name ILIKE '%' || @query || '%'
)
SELECT match.kind, match.ref, match.project_ref, match.title, match.subtitle, match.state,
       match.updated_at, match.relevance, match.order_time
FROM matches AS match
ORDER BY match.relevance, match.order_time DESC, match.kind, match.ref;
