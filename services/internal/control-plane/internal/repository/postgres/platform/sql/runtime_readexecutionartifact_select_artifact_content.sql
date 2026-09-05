-- name: runtime_readexecutionartifact_select_artifact_content :one
SELECT artifact.ref,
       COALESCE(project.ref, ''),
       COALESCE(artifact_run.ref, ''),
       COALESCE(artifact_session.ref, ''),
       artifact.file_name,
       artifact.media_type,
       artifact.digest,
       artifact.scan_state,
       artifact.preview_state,
       artifact.source,
       artifact.size_bytes,
       artifact.revision,
       (exact_snapshot.item ->> 'version')::bigint,
       artifact.created_at,
       content.object_key,
       content.object_version,
       content.object_etag,
       content.digest,
       content.size_bytes
FROM control_plane.runtime_leases AS lease
JOIN control_plane.runs AS run
  ON run.id = lease.run_id
JOIN control_plane.runs AS root_run
  ON root_run.id = run.root_run_id
JOIN control_plane.run_nodes AS node
  ON node.id = lease.node_id
JOIN control_plane.runtime_revisions AS revision
  ON revision.id = lease.runtime_revision_id
LEFT JOIN control_plane.session_turns AS turn
  ON turn.id = node.turn_id
JOIN control_plane.agents AS agent
  ON agent.id = node.agent_id
JOIN control_plane.artifacts AS artifact
  ON artifact.organization_id = lease.organization_id
 AND artifact.project_id IS NOT DISTINCT FROM run.project_id
LEFT JOIN control_plane.projects AS project
  ON project.id = artifact.project_id
JOIN control_plane.artifact_content AS content
  ON content.artifact_id = artifact.id
JOIN LATERAL (
    SELECT candidates.item FROM (
    SELECT exact.item,0 AS priority,exact.ordinal
    FROM jsonb_array_elements(COALESCE(revision.safe_snapshot -> 'artifacts', '[]'::jsonb))
         WITH ORDINALITY AS exact(item, ordinal)
    WHERE exact.item ->> 'ref' = artifact.ref
      AND exact.item ->> 'digest' = artifact.digest
      AND exact.item ->> 'digest' = content.digest
      AND exact.item -> 'revision' = to_jsonb(artifact.revision)
      AND exact.item ->> 'fileName' = artifact.file_name
      AND exact.item ->> 'mediaType' = artifact.media_type
      AND exact.item -> 'sizeBytes' = to_jsonb(artifact.size_bytes)
      AND exact.item ->> 'source' = artifact.source
      AND content.size_bytes = artifact.size_bytes
    UNION ALL
    SELECT jsonb_build_object('version',artifact.version),1,0::bigint
    FROM jsonb_array_elements(COALESCE(revision.safe_snapshot #> '{contextSnapshot,skills}','[]'::jsonb)) AS skill(item)
    JOIN control_plane.agent_context_bindings binding
      ON binding.organization_id=lease.organization_id AND binding.agent_id=agent.id
     AND binding.ref=skill.item->>'binding_ref' AND to_jsonb(binding.version)=skill.item->'binding_version' AND binding.enabled
    JOIN control_plane.skill_bundles bundle
      ON bundle.id=binding.skill_bundle_id AND bundle.ref=skill.item->>'bundle_ref' AND bundle.state='ACTIVE'
    JOIN control_plane.skill_bundle_revisions skill_revision
      ON skill_revision.id=binding.skill_revision_id AND skill_revision.bundle_id=bundle.id
     AND skill_revision.ref=skill.item->>'revision_ref' AND skill_revision.digest=skill.item->>'digest'
     AND skill_revision.state='PUBLISHED' AND skill_revision.scan_state='CLEAN'
    JOIN control_plane.subjects actor ON actor.id=root_run.initiated_by AND actor.organization_id=lease.organization_id AND actor.active
    JOIN control_plane.catalog_access_targets project_target ON project_target.organization_id=lease.organization_id AND project_target.kind='PROJECT' AND project_target.id=run.project_id
    JOIN control_plane.catalog_access_targets agent_target ON agent_target.organization_id=lease.organization_id AND agent_target.kind='AGENT' AND agent_target.id=agent.id
    WHERE artifact.lifecycle_state='ACTIVE' AND content.digest=artifact.digest AND content.size_bytes=artifact.size_bytes
      AND binding.project_id=run.project_id AND bundle.project_id=run.project_id
      AND control_plane.catalog_resource_visible(lease.organization_id,actor.id,'project.view','PROJECT',project_target.id,project_target.project_id,project_target.owner_id,project_target.related_ids,statement_timestamp(),false)
      AND control_plane.catalog_resource_visible(lease.organization_id,actor.id,'agent.view','AGENT',agent_target.id,agent_target.project_id,agent_target.owner_id,agent_target.related_ids,statement_timestamp(),false)
      AND control_plane.skill_revision_visible(lease.organization_id,actor.id,skill_revision.id,statement_timestamp())
      AND EXISTS (
          SELECT 1 FROM jsonb_array_elements(skill.item->'files') AS file(item)
          WHERE file.item->>'artifact_ref'=artifact.ref AND file.item->>'digest'=artifact.digest
            AND file.item->'artifact_revision'=to_jsonb(artifact.revision)
            AND file.item->'size_bytes'=to_jsonb(artifact.size_bytes))
    ) candidates
    ORDER BY candidates.priority,candidates.ordinal
    LIMIT 1
) AS exact_snapshot ON true
LEFT JOIN control_plane.runs AS artifact_run
  ON artifact_run.id = artifact.run_id
LEFT JOIN control_plane.sessions AS artifact_session
  ON artifact_session.id = artifact_run.session_id
WHERE lease.organization_id = @organization_id::uuid
  AND lease.ref = @lease_ref
  AND lease.fence_digest = @fence_digest
  AND lease.generation = @generation
  AND lease.state = 'CLAIMED'
  AND lease.expires_at > clock_timestamp()
  AND artifact.ref = @artifact_ref
  AND artifact.scan_state = 'CLEAN'
  AND artifact.lifecycle_state IN ('ACTIVE', 'DELETED')
