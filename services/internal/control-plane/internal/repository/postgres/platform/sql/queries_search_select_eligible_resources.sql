-- name: queries_search_select_eligible_resources :many
WITH eligible_projects AS (
    SELECT project.id, project.ref
    FROM control_plane.projects AS project
    WHERE project.organization_id = $1::uuid
      AND project.lifecycle <> 'ARCHIVED'
      AND (
          $2 IN ('OWNER', 'ADMINISTRATOR')
          OR EXISTS (
              SELECT 1
              FROM control_plane.memberships AS membership
              WHERE membership.project_id = project.id
                AND membership.subject_id = $3::uuid
                AND membership.active
                AND 'VIEW' = ANY(membership.permissions)
          )
      )
), matches AS (
    SELECT 'PROJECT'::text AS kind,
           project.ref,
           project.ref AS project_ref,
           project.name AS title,
           project.purpose AS subtitle,
           project.lifecycle AS state,
           project.updated_at,
           CASE
               WHEN lower(project.name) = lower($4) THEN 0
               WHEN project.name ILIKE $4 || '%' THEN 1
               ELSE 2
           END AS relevance
    FROM control_plane.projects AS project
    JOIN eligible_projects AS eligible ON eligible.id = project.id
    WHERE project.name ILIKE '%' || $4 || '%'
       OR project.purpose ILIKE '%' || $4 || '%'

    UNION ALL

    SELECT 'AGENT', agent.ref, project.ref, agent.name, agent.purpose,
           agent.state, agent.updated_at,
           CASE
               WHEN lower(agent.name) = lower($4) THEN 0
               WHEN agent.name ILIKE $4 || '%' THEN 1
               ELSE 2
           END
    FROM control_plane.agents AS agent
    JOIN eligible_projects AS project ON project.id = agent.project_id
    WHERE agent.system_key IS NULL
      AND agent.state <> 'ARCHIVED'
      AND (agent.name ILIKE '%' || $4 || '%' OR agent.purpose ILIKE '%' || $4 || '%')

    UNION ALL

    SELECT 'WORKFLOW', workflow.ref, project.ref, workflow.name,
           workflow.purpose, workflow.state, workflow.updated_at,
           CASE
               WHEN lower(workflow.name) = lower($4) THEN 0
               WHEN workflow.name ILIKE $4 || '%' THEN 1
               ELSE 2
           END
    FROM control_plane.workflows AS workflow
    JOIN eligible_projects AS project ON project.id = workflow.project_id
    WHERE workflow.state <> 'ARCHIVED'
      AND (workflow.name ILIKE '%' || $4 || '%' OR workflow.purpose ILIKE '%' || $4 || '%')

    UNION ALL

    SELECT 'RUN', run.ref, project.ref, run.title, run.task,
           run.state, run.updated_at,
           CASE
               WHEN lower(run.title) = lower($4) THEN 0
               WHEN run.title ILIKE $4 || '%' THEN 1
               ELSE 2
           END
    FROM control_plane.runs AS run
    JOIN eligible_projects AS project ON project.id = run.project_id
    WHERE run.title ILIKE '%' || $4 || '%'
       OR run.task ILIKE '%' || $4 || '%'
)
SELECT kind, ref, project_ref, title, subtitle, state, updated_at
FROM matches
ORDER BY relevance, updated_at DESC, kind, ref
LIMIT $5
