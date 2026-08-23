-- name: runtime_readexecutionartifact_select_artifact_content :one
SELECT artifact.ref,
       project.ref,
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
       artifact.version,
       artifact.created_at,
       content.body
FROM control_plane.runtime_leases AS lease
JOIN control_plane.runs AS run
  ON run.id = lease.run_id
JOIN control_plane.runs AS root_run
  ON root_run.id = run.root_run_id
JOIN control_plane.run_nodes AS node
  ON node.id = lease.node_id
JOIN control_plane.agents AS agent
  ON agent.id = node.agent_id
JOIN control_plane.artifacts AS artifact
  ON artifact.organization_id = lease.organization_id
 AND artifact.project_id = run.project_id
JOIN control_plane.projects AS project
  ON project.id = artifact.project_id
JOIN control_plane.artifact_content AS content
  ON content.artifact_id = artifact.id
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
  AND (
    artifact.ref = ANY(root_run.input_artifact_refs)
    OR EXISTS (
      SELECT 1
      FROM control_plane.artifact_bindings AS binding
      WHERE binding.artifact_id = artifact.id
        AND binding.target_kind = 'KNOWLEDGE'
        AND binding.target_ref = agent.ref
    )
  )
