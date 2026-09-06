-- name: vfs_list_nodes :many
WITH projects AS (
    SELECT project.id, project.ref, project.name, project.updated_at
    FROM control_plane.projects AS project
    WHERE project.organization_id = @organization_id::uuid
      AND project.lifecycle <> 'ARCHIVED'
      AND (@project_ref = '' OR project.ref = @project_ref)
), context_bindings AS (
    SELECT binding.ref,agent.ref AS agent_ref,project.ref AS project_ref,bundle.ref AS entity_ref,
           revision.name,'SKILL'::text AS kind,'skills'::text AS folder,revision.digest,0::bigint AS size_bytes,binding.updated_at
    FROM control_plane.agent_context_bindings binding
    JOIN projects project ON project.id=binding.project_id
    JOIN control_plane.agents agent ON agent.id=binding.agent_id AND agent.project_id=binding.project_id
    JOIN control_plane.skill_bundles bundle ON bundle.id=binding.skill_bundle_id AND bundle.project_id=binding.project_id
    JOIN control_plane.skill_bundle_revisions revision ON revision.id=binding.skill_revision_id AND revision.bundle_id=bundle.id
    JOIN control_plane.catalog_access_targets target ON target.organization_id=binding.organization_id AND target.kind='PROJECT' AND target.id=binding.project_id
    WHERE binding.organization_id=@organization_id::uuid AND binding.enabled
      AND agent.system_key IS NULL AND agent.state<>'ARCHIVED' AND bundle.state='ACTIVE'
      AND revision.state='PUBLISHED' AND revision.scan_state='CLEAN'
      AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,'project.view','PROJECT',
          target.id,target.project_id,target.owner_id,target.related_ids,@evaluated_at,false)
      AND control_plane.skill_revision_visible(@organization_id::uuid,@actor_id::uuid,revision.id,@evaluated_at)
    UNION ALL
    SELECT binding.ref,agent.ref,project.ref,memory.ref,revision.title,'MEMORY','memories',revision.digest,
           octet_length(revision.summary)::bigint,binding.updated_at
    FROM control_plane.agent_context_bindings binding
    JOIN projects project ON project.id=binding.project_id
    JOIN control_plane.agents agent ON agent.id=binding.agent_id AND agent.project_id=binding.project_id
    JOIN control_plane.memory_records memory ON memory.id=binding.memory_record_id AND memory.project_id=binding.project_id
    JOIN control_plane.memory_record_revisions revision ON revision.id=binding.memory_revision_id AND revision.record_id=memory.id
    JOIN control_plane.memory_record_revisions current ON current.id=memory.current_revision_id
    WHERE binding.organization_id=@organization_id::uuid AND binding.enabled
      AND agent.system_key IS NULL AND agent.state<>'ARCHIVED' AND memory.state='ACTIVE'
      AND revision.retention_until>@evaluated_at AND current.retention_until>@evaluated_at
      AND control_plane.memory_record_visible(@organization_id::uuid,@actor_id::uuid,memory.id,@evaluated_at)
      AND (revision.source_run_id IS NULL OR EXISTS (
          SELECT 1 FROM control_plane.catalog_access_targets source
          WHERE source.organization_id=binding.organization_id AND source.kind='RUN' AND source.id=revision.source_run_id
            AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,'run.view','RUN',
                source.id,source.project_id,source.owner_id,source.related_ids,@evaluated_at,false)))
), knowledge_files AS (
    SELECT agent.ref AS agent_ref, project.ref AS project_ref, artifact.ref AS artifact_ref,
           artifact.file_name, artifact.size_bytes, artifact.digest, artifact.created_at
    FROM control_plane.artifact_bindings binding
    JOIN control_plane.agents agent ON agent.ref=binding.target_ref
    JOIN projects project ON project.id=agent.project_id
    JOIN control_plane.artifacts artifact ON artifact.id=binding.artifact_id
      AND artifact.organization_id=agent.organization_id AND artifact.project_id=agent.project_id
    WHERE binding.target_kind='KNOWLEDGE' AND agent.organization_id=@organization_id::uuid
      AND agent.system_key IS NULL AND agent.state<>'ARCHIVED'
      AND artifact.lifecycle_state=@lifecycle_state AND artifact.scan_state='CLEAN'
      AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,'agent.view','AGENT',
          agent.id,agent.project_id,agent.created_by,jsonb_build_object('PROJECT',agent.project_id::text),@evaluated_at,false)
), nodes AS (
    SELECT 'context-binding:' || binding.ref AS ref,
           '/projects/' || binding.project_ref || '/agents/' || binding.agent_ref || '/' || binding.folder || '/' || binding.ref AS path,
           '/projects/' || binding.project_ref || '/agents/' || binding.agent_ref || '/' || binding.folder AS parent_path,
           binding.name,binding.kind,binding.kind='SKILL' AS directory,binding.project_ref,binding.entity_ref,''::text AS run_ref,
           binding.size_bytes,binding.digest,binding.updated_at AS modified_at,'AGENT'::text AS access_kind,binding.agent_ref AS access_ref
    FROM context_bindings binding
    UNION ALL
    SELECT 'dir:' || binding.agent_ref || ':' || binding.folder,
           '/projects/' || binding.project_ref || '/agents/' || binding.agent_ref || '/' || binding.folder,
           '/projects/' || binding.project_ref || '/agents/' || binding.agent_ref,
           binding.folder,'DIRECTORY',true,binding.project_ref,'','',0,'',max(binding.updated_at),'AGENT',binding.agent_ref
    FROM context_bindings binding GROUP BY binding.project_ref,binding.agent_ref,binding.folder
    UNION ALL
    SELECT 'knowledge-input:'||file.agent_ref||':'||file.artifact_ref,
           '/projects/'||file.project_ref||'/agents/'||file.agent_ref||'/workspace/inputs/'||file.artifact_ref,
           '/projects/'||file.project_ref||'/agents/'||file.agent_ref||'/workspace/inputs',
           file.file_name,'INPUT',false,file.project_ref,file.artifact_ref,'',file.size_bytes,file.digest,file.created_at,'ARTIFACT',file.artifact_ref
    FROM knowledge_files file
    UNION ALL
    SELECT 'dir:'||file.agent_ref||':'||directory.name,
           '/projects/'||file.project_ref||'/agents/'||file.agent_ref||'/'||directory.name,
           '/projects/'||file.project_ref||'/agents/'||file.agent_ref||CASE WHEN directory.name='workspace' THEN '' ELSE '/workspace' END,
           CASE WHEN directory.name='workspace' THEN 'workspace' ELSE 'inputs' END,'DIRECTORY',true,file.project_ref,'','',0,'',max(file.created_at),'AGENT',file.agent_ref
    FROM knowledge_files file CROSS JOIN (VALUES ('workspace'),('workspace/inputs')) directory(name)
    GROUP BY file.project_ref,file.agent_ref,directory.name
    UNION ALL
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
    FROM projects AS project CROSS JOIN (VALUES ('runs'),('skills'),('memories'),('files')) directory(name)
    UNION ALL
    SELECT 'skill:' || bundle.ref, '/projects/' || project.ref || '/skills/' || bundle.ref,
           '/projects/' || project.ref || '/skills', revision.name, 'SKILL', true,
           project.ref, bundle.ref, '', 0, revision.digest, bundle.updated_at, 'PROJECT', project.ref
    FROM control_plane.skill_bundles bundle
    JOIN projects project ON project.id=bundle.project_id
    JOIN control_plane.skill_bundle_revisions revision ON revision.id=COALESCE(bundle.draft_revision_id,bundle.current_revision_id)
      AND revision.bundle_id=bundle.id AND revision.organization_id=bundle.organization_id
    WHERE bundle.state=CASE WHEN @lifecycle_state='DELETED' THEN 'ARCHIVED' ELSE 'ACTIVE' END
      AND EXISTS (
          SELECT 1 FROM control_plane.catalog_access_targets target
          WHERE target.organization_id=bundle.organization_id AND target.kind='PROJECT' AND target.id=bundle.project_id
            AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,'project.view','PROJECT',
                target.id,target.project_id,target.owner_id,target.related_ids,@evaluated_at,false))
      AND control_plane.skill_revision_visible(@organization_id::uuid,@actor_id::uuid,bundle.current_revision_id,@evaluated_at)
      AND control_plane.skill_revision_visible(@organization_id::uuid,@actor_id::uuid,bundle.draft_revision_id,@evaluated_at)
    UNION ALL
    SELECT 'memory:' || memory.ref, '/projects/' || project.ref || '/memories/' || memory.ref,
           '/projects/' || project.ref || '/memories', revision.title, 'MEMORY', false,
           project.ref, memory.ref, COALESCE(source.ref,''), octet_length(revision.summary)::bigint,
           revision.digest, memory.updated_at,
           CASE WHEN memory.agent_id IS NULL THEN 'PROJECT' ELSE 'AGENT' END,
           COALESCE(agent.ref,project.ref)
    FROM control_plane.memory_records memory
    JOIN projects project ON project.id=memory.project_id
    JOIN control_plane.memory_record_revisions revision ON revision.id=memory.current_revision_id
      AND revision.record_id=memory.id AND revision.organization_id=memory.organization_id
    LEFT JOIN control_plane.agents agent ON agent.id=memory.agent_id AND agent.project_id=memory.project_id
    LEFT JOIN control_plane.runs source ON source.id=revision.source_run_id AND source.organization_id=memory.organization_id
    WHERE memory.state=CASE WHEN @lifecycle_state='DELETED' THEN 'ARCHIVED' ELSE 'ACTIVE' END AND revision.retention_until>@evaluated_at
      AND control_plane.memory_record_visible(@organization_id::uuid,@actor_id::uuid,memory.id,@evaluated_at)
    UNION ALL
    SELECT 'dir:' || project.ref || ':' || directory.name,
           '/projects/' || project.ref || '/' || directory.name,
           '/projects/' || project.ref || '', directory.name, 'DIRECTORY', true,
           project.ref, '', '', 0, '', project.updated_at, 'PROJECT', project.ref
    FROM projects AS project CROSS JOIN (VALUES ('agents'),('workflows'),('automations'),('environments')) directory(name)
    UNION ALL
    SELECT 'agent:' || agent.ref, '/projects/' || project.ref || '/agents/' || agent.ref,
           '/projects/' || project.ref || '/agents', agent.name, 'AGENT', true,
           project.ref, agent.ref, '', 0, '', agent.updated_at, 'AGENT', agent.ref
    FROM control_plane.agents AS agent JOIN projects AS project ON project.id = agent.project_id
    WHERE agent.system_key IS NULL AND agent.state <> 'ARCHIVED'
    UNION ALL
    SELECT 'workflow:' || workflow.ref, '/projects/' || project.ref || '/workflows/' || workflow.ref,
           '/projects/' || project.ref || '/workflows', workflow.name, 'WORKFLOW', true,
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
    WHERE artifact.lifecycle_state = @lifecycle_state AND artifact.scan_state = 'CLEAN'
    UNION ALL
    SELECT 'artifact-result:' || run.ref || ':' || artifact.ref,
           '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/results/' || artifact.ref,
           '/projects/' || project.ref || '/runs/' || run.ref || '/workspace/results',
           artifact.file_name, 'RESULT', false, project.ref, artifact.ref, run.ref,
           artifact.size_bytes, artifact.digest, artifact.created_at, 'ARTIFACT', artifact.ref
    FROM control_plane.artifacts AS artifact
    JOIN control_plane.runs AS run ON run.id = artifact.run_id
    JOIN projects AS project ON project.id = artifact.project_id
    WHERE artifact.lifecycle_state = @lifecycle_state
      AND artifact.source IN ('AGENT_RESULT', 'INTEGRATION_RESULT')
    UNION ALL
    SELECT 'file:' || artifact.ref,
           '/projects/' || project.ref || '/files/' || artifact.ref,
           '/projects/' || project.ref || '/files', artifact.file_name, 'INPUT', false,
           project.ref, artifact.ref, '', artifact.size_bytes, artifact.digest, artifact.created_at,
           'ARTIFACT', artifact.ref
    FROM control_plane.artifacts AS artifact JOIN projects AS project ON project.id = artifact.project_id
    WHERE artifact.run_id IS NULL AND artifact.lifecycle_state = @lifecycle_state
    UNION ALL
    SELECT 'environment:' || environment.ref,
           '/projects/' || project.ref || '/environments/' || environment.ref,
           '/projects/' || project.ref || '/environments', environment.name, 'ENVIRONMENT', true,
           project.ref, environment.ref, '', 0, version.digest, version.created_at,
           'PROJECT', project.ref
    FROM control_plane.runtime_environment_sets AS environment
    JOIN projects AS project ON project.id = environment.project_id
    JOIN control_plane.runtime_environment_versions AS version ON version.id = environment.current_version_id
    WHERE environment.state <> 'DELETED'
    UNION ALL
    SELECT 'automation:' || schedule.ref,
           '/projects/' || project.ref || '/automations/' || schedule.ref,
           '/projects/' || project.ref || '/automations', schedule.name, 'AUTOMATION', true,
           project.ref, schedule.ref, '', 0, revision.digest, schedule.updated_at, 'SCHEDULE', schedule.ref
    FROM control_plane.schedules schedule
    JOIN projects project ON project.id = schedule.project_id
    JOIN control_plane.schedule_revisions revision ON revision.id = schedule.current_revision_id
    WHERE schedule.lifecycle_state <> 'DELETED'
    UNION ALL
    SELECT 'avatar:' || agent.ref || ':' || artifact.ref,
           '/projects/' || project.ref || '/agents/' || agent.ref || '/avatar',
           '/projects/' || project.ref || '/agents/' || agent.ref,
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
), visible AS MATERIALIZED (
    SELECT filtered.*
    FROM nodes filtered JOIN control_plane.catalog_access_targets target
      ON target.organization_id = @organization_id::uuid AND target.kind = filtered.access_kind AND target.ref = filtered.access_ref
    WHERE (@authority_project = '' OR target.project_id = NULLIF(@authority_project, '')::uuid)
      AND control_plane.catalog_resource_visible(@organization_id::uuid, @actor_id::uuid,
          CASE filtered.access_kind WHEN 'PROJECT' THEN 'project.view' WHEN 'AGENT' THEN 'agent.view'
            WHEN 'WORKFLOW' THEN 'workflow.view' WHEN 'RUN' THEN 'run.view' WHEN 'ARTIFACT' THEN 'artifact.view'
            WHEN 'SCHEDULE' THEN 'schedule.view' ELSE '' END,
          target.kind, target.id, target.project_id, target.owner_id, target.related_ids, @evaluated_at, filtered.access_kind IN ('PROJECT','ARTIFACT'))
      AND (filtered.run_ref = '' OR filtered.access_kind = 'RUN' OR EXISTS (
          SELECT 1 FROM control_plane.catalog_access_targets parent
          WHERE parent.organization_id = @organization_id::uuid AND parent.kind = 'RUN' AND parent.ref = filtered.run_ref
            AND control_plane.catalog_resource_visible(@organization_id::uuid, @actor_id::uuid, 'run.view', 'RUN',
                parent.id, parent.project_id, parent.owner_id, parent.related_ids, @evaluated_at, false)
      ))
), skill_files AS MATERIALIZED (
    SELECT parent.ref AS parent_ref,parent.path AS source_path,parent.project_ref,parent.run_ref,
           file.item->>'Path' AS file_path,artifact.ref AS artifact_ref,artifact.file_name,
           artifact.size_bytes,artifact.digest,artifact.created_at
    FROM visible parent
    JOIN control_plane.skill_bundles bundle ON bundle.organization_id=@organization_id::uuid AND bundle.ref=parent.entity_ref
    LEFT JOIN control_plane.agent_context_bindings binding ON binding.organization_id=@organization_id::uuid AND parent.ref='context-binding:'||binding.ref
    JOIN control_plane.skill_bundle_revisions revision ON revision.bundle_id=bundle.id
      AND revision.id=COALESCE(binding.skill_revision_id,bundle.draft_revision_id,bundle.current_revision_id)
    CROSS JOIN LATERAL jsonb_array_elements(revision.files) file(item)
    JOIN control_plane.artifacts artifact ON artifact.organization_id=@organization_id::uuid AND artifact.project_id=bundle.project_id
      AND artifact.ref=file.item->>'ArtifactRef' AND to_jsonb(artifact.revision)=file.item->'ArtifactRevision'
      AND artifact.digest=file.item->>'Digest' AND to_jsonb(artifact.size_bytes)=file.item->'SizeBytes'
    JOIN control_plane.catalog_access_targets target ON target.organization_id=artifact.organization_id AND target.kind='ARTIFACT' AND target.id=artifact.id
    WHERE parent.kind='SKILL' AND artifact.lifecycle_state='ACTIVE' AND artifact.scan_state='CLEAN'
      AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,'artifact.view','ARTIFACT',
          target.id,target.project_id,target.owner_id,target.related_ids,@evaluated_at)
), expanded AS MATERIALIZED (
    SELECT * FROM visible
    UNION ALL
    SELECT 'skill-file:'||file.parent_ref||':'||file.file_path,
           file.source_path||'/'||file.file_path,
           file.source_path||CASE WHEN strpos(file.file_path,'/')=0 THEN '' ELSE '/'||regexp_replace(file.file_path,'/[^/]+$','') END,
           regexp_replace(file.file_path,'^.*/',''),'INPUT',false,file.project_ref,file.artifact_ref,file.run_ref,
           file.size_bytes,file.digest,file.created_at,'ARTIFACT',file.artifact_ref
    FROM skill_files file
    UNION ALL
    SELECT 'skill-dir:'||file.parent_ref||':'||array_to_string(parts.items[1:depth.level],'/'),
           file.source_path||'/'||array_to_string(parts.items[1:depth.level],'/'),
           file.source_path||CASE WHEN depth.level=1 THEN '' ELSE '/'||array_to_string(parts.items[1:depth.level-1],'/') END,
           parts.items[depth.level],'DIRECTORY',true,file.project_ref,'','',0,'',max(file.created_at),'PROJECT',file.project_ref
    FROM skill_files file
    CROSS JOIN LATERAL (SELECT string_to_array(file.file_path,'/') AS items) parts
    CROSS JOIN LATERAL generate_series(1,cardinality(parts.items)-1) depth(level)
    GROUP BY file.parent_ref,file.source_path,file.project_ref,depth.level,
        parts.items[1:depth.level],parts.items[1:depth.level-1],parts.items[depth.level]
), enriched AS MATERIALIZED (
    SELECT visible.*,
        COALESCE(artifact.version,bundle.version,memory.version,0) AS version,
        COALESCE(artifact.revision,skill_revision.revision,memory_revision.revision,0) AS revision,
        COALESCE(skill_revision.ref,memory_revision.ref,'') AS revision_ref,
        COALESCE(artifact.lifecycle_state,bundle.state,memory.state,'ACTIVE') AS lifecycle_state,
        COALESCE(artifact.scan_state,skill_revision.scan_state,'') AS scan_state,
        CASE WHEN artifact.id IS NOT NULL THEN 'ARTIFACT' WHEN bundle.id IS NOT NULL THEN 'SKILL_BUNDLE'
            WHEN memory.id IS NOT NULL THEN 'MEMORY_RECORD' ELSE '' END AS resource_kind,
        EXISTS (
            SELECT 1 FROM control_plane.catalog_access_targets target
            WHERE target.organization_id=@organization_id::uuid AND target.kind=visible.access_kind AND target.ref=visible.access_ref
              AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,
                  CASE visible.access_kind WHEN 'PROJECT' THEN 'project.manage' WHEN 'AGENT' THEN 'agent.manage' ELSE '' END,
                  target.kind,target.id,target.project_id,target.owner_id,target.related_ids,@evaluated_at)
        ) AS can_manage
    FROM expanded visible
    LEFT JOIN control_plane.artifacts artifact ON artifact.organization_id=@organization_id::uuid AND visible.access_kind='ARTIFACT' AND artifact.ref=visible.entity_ref
    LEFT JOIN control_plane.agent_context_bindings binding ON binding.organization_id=@organization_id::uuid AND visible.ref='context-binding:'||binding.ref
    LEFT JOIN control_plane.skill_bundles bundle ON bundle.organization_id=@organization_id::uuid AND visible.kind='SKILL' AND bundle.ref=visible.entity_ref
    LEFT JOIN control_plane.skill_bundle_revisions skill_revision ON skill_revision.organization_id=@organization_id::uuid
      AND skill_revision.bundle_id=bundle.id AND skill_revision.id=COALESCE(binding.skill_revision_id,bundle.draft_revision_id,bundle.current_revision_id)
    LEFT JOIN control_plane.memory_records memory ON memory.organization_id=@organization_id::uuid AND visible.kind='MEMORY' AND memory.ref=visible.entity_ref
    LEFT JOIN control_plane.memory_record_revisions memory_revision ON memory_revision.organization_id=@organization_id::uuid
      AND memory_revision.record_id=memory.id AND memory_revision.id=COALESCE(binding.memory_revision_id,memory.current_revision_id)
), applicable AS MATERIALIZED (
    SELECT * FROM enriched candidate
    WHERE (candidate.directory AND candidate.resource_kind='' OR candidate.lifecycle_state=CASE WHEN @lifecycle_state='DELETED' AND candidate.resource_kind IN ('SKILL_BUNDLE','MEMORY_RECORD') THEN 'ARCHIVED' ELSE @lifecycle_state END)
      AND (candidate.kind <> 'DIRECTORY' OR EXISTS (
          SELECT 1 FROM enriched child WHERE child.kind<>'DIRECTORY' AND starts_with(child.path,candidate.path||'/')
            AND (child.directory AND child.resource_kind='' OR child.lifecycle_state=CASE WHEN @lifecycle_state='DELETED' AND child.resource_kind IN ('SKILL_BUNDLE','MEMORY_RECORD') THEN 'ARCHIVED' ELSE @lifecycle_state END)
      ))
), filtered AS MATERIALIZED (
    SELECT * FROM applicable
    WHERE ((@mode='TREE' AND parent_path=@path) OR (@mode='SEARCH' AND (@path='' OR starts_with(path,@path||'/') OR path=@path)))
      AND (@query='' OR strpos(lower(name),lower(@query))>0 OR (@mode='SEARCH' AND strpos(lower(path),lower(@query))>0))
      AND (COALESCE(cardinality(@kinds::text[]),0)=0 OR kind=ANY(@kinds::text[]))
), page AS (
    SELECT * FROM filtered
    WHERE (@cursor_path = '' OR (path, ref) > (@cursor_path, @cursor_ref))
    ORDER BY path, ref LIMIT @page_size
)
SELECT COALESCE(jsonb_agg(to_jsonb(page) ORDER BY page.path, page.ref), '[]'::jsonb), (SELECT count(*) FROM filtered)
FROM page;
