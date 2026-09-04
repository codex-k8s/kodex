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
    FROM projects AS project CROSS JOIN (VALUES ('entities'),('runs'),('skills'),('memories'),('files')) directory(name)
    UNION ALL
    SELECT 'dir:' || project.ref || ':entities:' || directory.name,
           '/projects/' || project.ref || '/entities/' || directory.name,
           '/projects/' || project.ref || '/entities', directory.name, 'DIRECTORY', true,
           project.ref, '', '', 0, '', project.updated_at, 'PROJECT', project.ref
    FROM projects AS project CROSS JOIN (VALUES ('agents'),('workflows'),('automations'),('environments')) directory(name)
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
    SELECT 'file:' || artifact.ref,
           '/projects/' || project.ref || '/files/' || artifact.ref,
           '/projects/' || project.ref || '/files', artifact.file_name, 'INPUT', false,
           project.ref, artifact.ref, '', artifact.size_bytes, artifact.digest, artifact.created_at,
           'ARTIFACT', artifact.ref
    FROM control_plane.artifacts AS artifact JOIN projects AS project ON project.id = artifact.project_id
    WHERE artifact.run_id IS NULL AND artifact.lifecycle_state = 'ACTIVE' AND artifact.scan_state = 'CLEAN'
    UNION ALL
    SELECT 'environment:' || environment.ref,
           '/projects/' || project.ref || '/entities/environments/' || environment.ref,
           '/projects/' || project.ref || '/entities/environments', environment.name, 'ENVIRONMENT', true,
           project.ref, environment.ref, '', 0, version.digest, version.created_at,
           'PROJECT', project.ref
    FROM control_plane.runtime_environment_sets AS environment
    JOIN projects AS project ON project.id = environment.project_id
    JOIN control_plane.runtime_environment_versions AS version ON version.id = environment.current_version_id
    WHERE environment.state <> 'DELETED'
    UNION ALL
    SELECT 'automation:' || schedule.ref,
           '/projects/' || project.ref || '/entities/automations/' || schedule.ref,
           '/projects/' || project.ref || '/entities/automations', schedule.name, 'AUTOMATION', true,
           project.ref, schedule.ref, '', 0, revision.digest, schedule.updated_at, 'SCHEDULE', schedule.ref
    FROM control_plane.schedules schedule
    JOIN projects project ON project.id = schedule.project_id
    JOIN control_plane.schedule_revisions revision ON revision.id = schedule.current_revision_id
    WHERE schedule.lifecycle_state <> 'DELETED'
    UNION ALL
    SELECT 'avatar:' || agent.ref || ':' || artifact.ref,
           '/projects/' || project.ref || '/entities/agents/' || agent.ref || '/avatar',
           '/projects/' || project.ref || '/entities/agents/' || agent.ref,
           artifact.file_name, 'AVATAR', false, project.ref, artifact.ref, '', artifact.size_bytes,
           artifact.digest, artifact.created_at, 'ARTIFACT', artifact.ref
    FROM control_plane.agents agent
    JOIN projects project ON project.id = agent.project_id
    JOIN control_plane.artifacts artifact ON artifact.id = agent.avatar_artifact_id
      AND artifact.organization_id = agent.organization_id AND artifact.project_id = agent.project_id
      AND artifact.revision = agent.avatar_artifact_revision
    WHERE agent.system_key IS NULL AND agent.state <> 'ARCHIVED'
      AND artifact.lifecycle_state = 'ACTIVE' AND artifact.scan_state = 'CLEAN'
      AND control_plane.catalog_resource_visible(@organization_id::uuid, @actor_id::uuid, 'agent.view', 'AGENT',
          agent.id, agent.project_id, agent.created_by, jsonb_build_object('PROJECT', agent.project_id::text), @evaluated_at, false)
), filtered AS (
    SELECT * FROM nodes
    WHERE ((@mode = 'TREE' AND parent_path = @path)
        OR (@mode = 'SEARCH' AND (name ILIKE '%' || @query || '%' OR path ILIKE '%' || @query || '%')))
), visible AS MATERIALIZED (
    SELECT filtered.*
    FROM filtered JOIN control_plane.catalog_access_targets target
      ON target.organization_id = @organization_id::uuid AND target.kind = filtered.access_kind AND target.ref = filtered.access_ref
    WHERE (@authority_project = '' OR target.project_id = NULLIF(@authority_project, '')::uuid)
      AND control_plane.catalog_resource_visible(@organization_id::uuid, @actor_id::uuid,
          CASE filtered.access_kind WHEN 'PROJECT' THEN 'project.view' WHEN 'AGENT' THEN 'agent.view'
            WHEN 'WORKFLOW' THEN 'workflow.view' WHEN 'RUN' THEN 'run.view' WHEN 'ARTIFACT' THEN 'artifact.view'
            WHEN 'SCHEDULE' THEN 'schedule.view' ELSE '' END,
          target.kind, target.id, target.project_id, target.owner_id, target.related_ids, @evaluated_at, filtered.access_kind = 'PROJECT')
      AND (filtered.run_ref = '' OR filtered.access_kind = 'RUN' OR EXISTS (
          SELECT 1 FROM control_plane.catalog_access_targets parent
          WHERE parent.organization_id = @organization_id::uuid AND parent.kind = 'RUN' AND parent.ref = filtered.run_ref
            AND control_plane.catalog_resource_visible(@organization_id::uuid, @actor_id::uuid, 'run.view', 'RUN',
                parent.id, parent.project_id, parent.owner_id, parent.related_ids, @evaluated_at, false)
      ))
), page AS (
    SELECT * FROM visible
    WHERE (@cursor_path = '' OR (path, ref) > (@cursor_path, @cursor_ref))
    ORDER BY path, ref LIMIT @page_size
)
SELECT COALESCE(jsonb_agg(to_jsonb(page) ORDER BY page.path, page.ref), '[]'::jsonb), (SELECT count(*) FROM visible)
FROM page;
