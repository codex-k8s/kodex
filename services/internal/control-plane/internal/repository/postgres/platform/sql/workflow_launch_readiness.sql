-- name: workflow_launch_readiness :many
WITH selected AS (
 SELECT w.*, version.ref AS revision_ref,
   ARRAY(SELECT DISTINCT ref FROM (
      SELECT w.published_spec->>'CoordinatorAgentRef' AS ref
      UNION ALL SELECT step->>'AgentRef' FROM jsonb_array_elements(COALESCE(w.published_spec->'Steps','[]')) step
   ) agents WHERE ref IS NOT NULL) AS agent_refs
 FROM control_plane.workflows w
 LEFT JOIN control_plane.workflow_versions version ON version.workflow_id=w.id AND version.version_number=w.published_version
 WHERE w.organization_id=@organization_id::uuid AND w.ref=ANY(@workflow_refs::text[])
   AND (@authority_project='' OR w.project_id::text=@authority_project)
   AND control_plane.catalog_resource_visible(w.organization_id,@actor_id::uuid,'workflow.view','WORKFLOW',
      w.id,w.project_id,w.created_by,jsonb_build_object('PROJECT',w.project_id::text),transaction_timestamp())
)
SELECT w.ref,w.version,COALESCE(w.revision_ref,''),COALESCE(w.published_spec->>'CoordinatorAgentRef',''),
 CASE WHEN NOT control_plane.catalog_resource_visible(w.organization_id,@actor_id::uuid,'workflow.launch','WORKFLOW',
      w.id,w.project_id,w.created_by,jsonb_build_object('PROJECT',w.project_id::text),transaction_timestamp()) THEN 'PERMISSION_REQUIRED'
 WHEN w.state<>'PUBLISHED' OR w.revision_ref IS NULL THEN 'UNPUBLISHED'
 WHEN NOT control_plane.agent_runtime_contract_ready(w.organization_id,w.project_id,w.agent_refs,
      @contract_revision,@contract_digest) THEN 'DEPENDENCY_UNAVAILABLE'
 ELSE 'READY' END,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('agentVersion',a.version,'binding',to_jsonb(binding),
      'environment',to_jsonb(environment),'artifact',to_jsonb(artifact)) ORDER BY a.ref)
   FROM control_plane.agents a
   LEFT JOIN control_plane.agent_runtime_environment_bindings binding ON binding.agent_id=a.id
   LEFT JOIN control_plane.runtime_environment_versions environment ON environment.id=binding.environment_version_id
   LEFT JOIN control_plane.image_artifacts artifact ON artifact.id=environment.role_image_artifact_id
   WHERE a.organization_id=w.organization_id AND a.project_id=w.project_id AND a.ref=ANY(w.agent_refs)),'[]')
FROM selected w ORDER BY w.ref;
