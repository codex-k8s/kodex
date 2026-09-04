-- name: vfs_list_nodes :many
WITH eligible_projects AS (
    SELECT project.id, project.ref, project.name, project.updated_at
    FROM control_plane.projects project
    WHERE project.organization_id = @organization_id::uuid
      AND project.lifecycle <> 'ARCHIVED'
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND (@actor_platform_role IN ('OWNER', 'ADMINISTRATOR') OR EXISTS (
          SELECT 1 FROM control_plane.memberships membership
          WHERE membership.project_id = project.id AND membership.subject_id = @actor_id::uuid
            AND membership.active AND 'VIEW' = ANY(membership.permissions)))
), nodes AS (
    SELECT 'project:' || project.ref AS ref, '/projects/' || project.ref AS path,
           '/projects' AS parent_path, project.name AS name, 'PROJECT' AS kind, true AS directory,
           project.ref AS project_ref, project.ref AS entity_ref, '' AS run_ref,
           0::bigint AS size_bytes, '' AS digest, project.updated_at AS modified_at
    FROM eligible_projects project
    UNION ALL
    SELECT 'dir:' || project.ref || ':' || directory.name,
           '/projects/' || project.ref || '/' || directory.name,
           '/projects/' || project.ref, directory.name, 'DIRECTORY', true,
           project.ref, '', '', 0, '', project.updated_at
    FROM eligible_projects project CROSS JOIN (VALUES ('entities'),('runs'),('skills'),('memories')) directory(name)
    UNION ALL
    SELECT 'dir:' || project.ref || ':entities:' || directory.name,
           '/projects/' || project.ref || '/entities/' || directory.name,
           '/projects/' || project.ref || '/entities', directory.name, 'DIRECTORY', true,
           project.ref, '', '', 0, '', project.updated_at
    FROM eligible_projects project CROSS JOIN (VALUES ('agents'),('workflows')) directory(name)
    UNION ALL
    SELECT 'agent:' || agent.ref, '/projects/' || project.ref || '/entities/agents/' || agent.ref,
           '/projects/' || project.ref || '/entities/agents', agent.name, 'AGENT', true,
           project.ref, agent.ref, '', 0, '', agent.updated_at
    FROM control_plane.agents agent JOIN eligible_projects project ON project.id = agent.project_id
    WHERE agent.system_key IS NULL AND agent.state <> 'ARCHIVED'
    UNION ALL
    SELECT 'workflow:' || workflow.ref, '/projects/' || project.ref || '/entities/workflows/' || workflow.ref,
           '/projects/' || project.ref || '/entities/workflows', workflow.name, 'WORKFLOW', true,
           project.ref, workflow.ref, '', 0, '', workflow.updated_at
    FROM control_plane.workflows workflow JOIN eligible_projects project ON project.id = workflow.project_id
    WHERE workflow.state <> 'ARCHIVED'
    UNION ALL
    SELECT 'run:' || run.ref, '/projects/' || project.ref || '/runs/' || run.ref,
           '/projects/' || project.ref || '/runs', run.title, 'RUN', true,
           project.ref, run.ref, run.ref, 0, '', run.updated_at
    FROM control_plane.runs run JOIN eligible_projects project ON project.id = run.project_id
    UNION ALL
    SELECT 'dir:' || run.ref || ':' || directory.name,
           CASE WHEN directory.name = 'workspace'
                THEN '/projects/' || project.ref || '/runs/' || run.ref || '/workspace'
                ELSE '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/' || directory.name END,
           CASE WHEN directory.name = 'workspace' THEN '/projects/' || project.ref || '/runs/' || run.ref
                ELSE '/projects/' || project.ref || '/runs/' || run.ref || '/workspace' END,
           directory.name, 'DIRECTORY', true, project.ref, run.ref, run.ref, 0, '', run.updated_at
    FROM control_plane.runs run JOIN eligible_projects project ON project.id = run.project_id
    CROSS JOIN (VALUES ('workspace'),('inputs'),('results')) directory(name)
    UNION ALL
    SELECT 'artifact:' || artifact.ref,
           '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/' ||
             CASE WHEN EXISTS (SELECT 1 FROM control_plane.attachment_set_items item WHERE item.artifact_id = artifact.id)
                  THEN 'inputs/' ELSE 'results/' END || artifact.ref,
           '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/' ||
             CASE WHEN EXISTS (SELECT 1 FROM control_plane.attachment_set_items item WHERE item.artifact_id = artifact.id)
                  THEN 'inputs' ELSE 'results' END,
           artifact.file_name,
           CASE WHEN EXISTS (SELECT 1 FROM control_plane.attachment_set_items item WHERE item.artifact_id = artifact.id)
                THEN 'INPUT' ELSE 'RESULT' END,
           false, project.ref, artifact.ref, run.ref, artifact.size_bytes, artifact.digest, artifact.created_at
    FROM control_plane.artifacts artifact
    JOIN control_plane.runs run ON run.id = artifact.run_id
    JOIN eligible_projects project ON project.id = artifact.project_id
    WHERE artifact.lifecycle_state = 'ACTIVE' AND artifact.scan_state = 'CLEAN'
    UNION ALL
    SELECT 'memory:' || artifact.ref,
           '/projects/' || project.ref || '/memories/' || artifact.ref,
           '/projects/' || project.ref || '/memories', artifact.file_name, 'MEMORY', false,
           project.ref, artifact.ref, '', artifact.size_bytes, artifact.digest, artifact.created_at
    FROM control_plane.artifacts artifact JOIN eligible_projects project ON project.id = artifact.project_id
    WHERE artifact.source = 'KNOWLEDGE_SOURCE' AND artifact.lifecycle_state = 'ACTIVE' AND artifact.scan_state = 'CLEAN'
    UNION ALL
    SELECT 'skill:' || environment.ref || ':' || tool.value,
           '/projects/' || project.ref || '/skills/' || md5(tool.value),
           '/projects/' || project.ref || '/skills', tool.value, 'SKILL', false,
           project.ref, environment.ref, '', 0, version.digest, version.created_at
    FROM control_plane.runtime_environment_sets environment
    JOIN eligible_projects project ON project.id = environment.project_id
    JOIN control_plane.runtime_environment_versions version ON version.id = environment.current_version_id
    CROSS JOIN LATERAL jsonb_array_elements_text(version.selected_tools) tool(value)
    WHERE environment.state = 'ACTIVE' AND tool.value <> ''
), filtered AS (
    SELECT * FROM nodes
    WHERE ((@mode = 'TREE' AND parent_path = @path)
        OR (@mode = 'SEARCH' AND (name ILIKE '%' || @query || '%' OR path ILIKE '%' || @query || '%')))
), total AS (SELECT count(*)::bigint AS value FROM filtered)
SELECT filtered.ref, filtered.path, filtered.parent_path, filtered.name, filtered.kind,
       filtered.directory, filtered.project_ref, filtered.entity_ref, filtered.run_ref,
       filtered.size_bytes, filtered.digest, filtered.modified_at, total.value
FROM filtered CROSS JOIN total
WHERE @cursor_path = '' OR (filtered.path, filtered.ref) > (@cursor_path, @cursor_ref)
ORDER BY filtered.path, filtered.ref
LIMIT @page_size;
