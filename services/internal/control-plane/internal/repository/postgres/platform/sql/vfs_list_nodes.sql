-- name: vfs_list_nodes :many
WITH projects AS (
    SELECT project.id, project.ref, project.name, project.updated_at
    FROM control_plane.projects AS project
    WHERE project.organization_id = @organization_id::uuid
      AND project.lifecycle <> 'ARCHIVED'
      AND (@project_ref = '' OR project.ref = @project_ref)
), nodes AS (
    SELECT 'project:' || project.ref AS ref, '/projects/' || project.ref AS path,
           '/projects' AS parent_path, project.name AS name, 'PROJECT' AS kind, true AS directory,
           project.ref AS project_ref, project.ref AS entity_ref, '' AS run_ref,
           0::bigint AS size_bytes, '' AS digest, project.updated_at AS modified_at,
           'PROJECT'::text AS access_kind, project.ref AS access_ref
    FROM projects AS project
    UNION ALL
    SELECT 'dir:' || project.ref || ':' || directory.name,
           '/projects/' || project.ref || '/' || directory.name,
           '/projects/' || project.ref, directory.name, 'DIRECTORY', true,
           project.ref, '', '', 0, '', project.updated_at, 'PROJECT', project.ref
    FROM projects AS project CROSS JOIN (VALUES ('entities'),('runs'),('skills'),('memories')) directory(name)
    UNION ALL
    SELECT 'dir:' || project.ref || ':entities:' || directory.name,
           '/projects/' || project.ref || '/entities/' || directory.name,
           '/projects/' || project.ref || '/entities', directory.name, 'DIRECTORY', true,
           project.ref, '', '', 0, '', project.updated_at, 'PROJECT', project.ref
    FROM projects AS project CROSS JOIN (VALUES ('agents'),('workflows')) directory(name)
    UNION ALL
    SELECT 'agent:' || agent.ref, '/projects/' || project.ref || '/entities/agents/' || agent.ref,
           '/projects/' || project.ref || '/entities/agents', agent.name, 'AGENT', true,
           project.ref, agent.ref, '', 0, '', agent.updated_at, 'AGENT', agent.ref
    FROM control_plane.agents AS agent JOIN projects AS project ON project.id = agent.project_id
    WHERE agent.system_key IS NULL AND agent.state <> 'ARCHIVED'
    UNION ALL
    SELECT 'workflow:' || workflow.ref, '/projects/' || project.ref || '/entities/workflows/' || workflow.ref,
           '/projects/' || project.ref || '/entities/workflows', workflow.name, 'WORKFLOW', true,
           project.ref, workflow.ref, '', 0, '', workflow.updated_at, 'WORKFLOW', workflow.ref
    FROM control_plane.workflows AS workflow JOIN projects AS project ON project.id = workflow.project_id
    WHERE workflow.state <> 'ARCHIVED'
    UNION ALL
    SELECT 'run:' || run.ref, '/projects/' || project.ref || '/runs/' || run.ref,
           '/projects/' || project.ref || '/runs', run.title, 'RUN', true,
           project.ref, run.ref, run.ref, 0, '', run.updated_at, 'RUN', run.ref
    FROM control_plane.runs AS run JOIN projects AS project ON project.id = run.project_id
    UNION ALL
    SELECT 'dir:' || run.ref || ':' || directory.name,
           CASE WHEN directory.name = 'workspace'
                THEN '/projects/' || project.ref || '/runs/' || run.ref || '/workspace'
                ELSE '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/' || directory.name END,
           CASE WHEN directory.name = 'workspace' THEN '/projects/' || project.ref || '/runs/' || run.ref
                ELSE '/projects/' || project.ref || '/runs/' || run.ref || '/workspace' END,
           directory.name, 'DIRECTORY', true, project.ref, run.ref, run.ref, 0, '', run.updated_at,
           'RUN', run.ref
    FROM control_plane.runs AS run JOIN projects AS project ON project.id = run.project_id
    CROSS JOIN (VALUES ('workspace'),('inputs'),('results')) directory(name)
    UNION ALL
    SELECT DISTINCT 'artifact-input:' || run.ref || ':' || item.artifact_ref,
           '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/inputs/' || item.artifact_ref,
           '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/inputs',
           item.file_name, 'INPUT', false, project.ref, item.artifact_ref, run.ref,
           item.size_bytes, item.digest, artifact.created_at, 'ARTIFACT', item.artifact_ref
    FROM control_plane.runs AS run
    JOIN projects AS project ON project.id = run.project_id
    JOIN control_plane.attachment_bindings AS binding
      ON binding.organization_id = run.organization_id
     AND (binding.run_id = run.id OR EXISTS (
          SELECT 1 FROM control_plane.session_turns AS turn
          WHERE turn.id = binding.session_turn_id AND turn.run_id = run.id))
    JOIN control_plane.attachment_sets AS attachment_set
      ON attachment_set.id = binding.attachment_set_id AND attachment_set.state = 'FINALIZED'
    JOIN control_plane.attachment_set_items AS item ON item.attachment_set_id = attachment_set.id
    JOIN control_plane.artifacts AS artifact
      ON artifact.id = item.artifact_id
     AND artifact.organization_id = run.organization_id
     AND artifact.project_id = run.project_id
     AND artifact.ref = item.artifact_ref
     AND artifact.revision = item.artifact_revision
     AND artifact.file_name = item.file_name
     AND artifact.media_type = item.media_type
     AND artifact.size_bytes = item.size_bytes
     AND artifact.digest = item.digest
     AND artifact.source = item.source
    WHERE artifact.lifecycle_state = 'ACTIVE' AND artifact.scan_state = 'CLEAN'
    UNION ALL
    SELECT 'artifact-result:' || run.ref || ':' || artifact.ref,
           '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/results/' || artifact.ref,
           '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/results',
           artifact.file_name, 'RESULT', false, project.ref, artifact.ref, run.ref,
           artifact.size_bytes, artifact.digest, artifact.created_at, 'ARTIFACT', artifact.ref
    FROM control_plane.artifacts AS artifact
    JOIN control_plane.runs AS run ON run.id = artifact.run_id
    JOIN projects AS project ON project.id = artifact.project_id
    WHERE artifact.lifecycle_state = 'ACTIVE' AND artifact.scan_state = 'CLEAN'
      AND artifact.source IN ('AGENT_RESULT', 'INTEGRATION_RESULT')
    UNION ALL
    SELECT 'memory:' || artifact.ref,
           '/projects/' || project.ref || '/memories/' || artifact.ref,
           '/projects/' || project.ref || '/memories', artifact.file_name, 'MEMORY', false,
           project.ref, artifact.ref, '', artifact.size_bytes, artifact.digest, artifact.created_at,
           'ARTIFACT', artifact.ref
    FROM control_plane.artifacts AS artifact JOIN projects AS project ON project.id = artifact.project_id
    WHERE artifact.source = 'KNOWLEDGE_SOURCE' AND artifact.lifecycle_state = 'ACTIVE' AND artifact.scan_state = 'CLEAN'
    UNION ALL
    SELECT 'skill:' || environment.ref || ':' || tool.value,
           '/projects/' || project.ref || '/skills/' || md5(tool.value),
           '/projects/' || project.ref || '/skills', tool.value, 'SKILL', false,
           project.ref, environment.ref, '', 0, version.digest, version.created_at,
           'PROJECT', project.ref
    FROM control_plane.runtime_environment_sets AS environment
    JOIN projects AS project ON project.id = environment.project_id
    JOIN control_plane.runtime_environment_versions AS version ON version.id = environment.current_version_id
    CROSS JOIN LATERAL jsonb_array_elements_text(version.selected_tools) tool(value)
    WHERE environment.state = 'ACTIVE' AND tool.value <> ''
), filtered AS (
    SELECT * FROM nodes
    WHERE ((@mode = 'TREE' AND parent_path = @path)
        OR (@mode = 'SEARCH' AND (name ILIKE '%' || @query || '%' OR path ILIKE '%' || @query || '%')))
)
SELECT filtered.ref, filtered.path, filtered.parent_path, filtered.name, filtered.kind,
       filtered.directory, filtered.project_ref, filtered.entity_ref, filtered.run_ref,
       filtered.size_bytes, filtered.digest, filtered.modified_at,
       filtered.access_kind, filtered.access_ref
FROM filtered
ORDER BY filtered.path, filtered.ref;
