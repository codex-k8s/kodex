-- name: runtime_claimexecution_select_claimable_agent_executions :many
SELECT n.id::text,
       n.ref,
       n.run_id::text,
       r.ref,
       r.root_run_id::text,
       COALESCE(r.project_id::text, ''),
       COALESCE(p.ref, ''),
       r.session_id::text,
       s.ref,
       COALESCE(t.content,r.task),
       a.ref,
       a.runtime_key,
       rp.runtime_revision,
       rp.provider,
       rp.model,
       pa.id::text,
       pa.ref,
       pcr.id::text,
       pcr.ref,
       pcr.revision_number,
       pcr.secret_name,
       pcr.secret_uid::text,
       pcr.secret_resource_version,
       pcr.content_sha256,
       iv.ref,
       iv.digest,
       iv.content || CASE
           WHEN a.system_key = 'system-assistant' AND COALESCE(ar.owner_instructions, '') <> ''
               THEN E'\n\n<owner-instructions>\n' || ar.owner_instructions || E'\n</owner-instructions>'
           ELSE ''
       END,
       a.capabilities,
       COALESCE((SELECT array_agg(knowledge_artifact.ref ORDER BY knowledge_binding.created_at)
                 FROM control_plane.artifact_bindings knowledge_binding
                 JOIN control_plane.artifacts knowledge_artifact ON knowledge_artifact.id=knowledge_binding.artifact_id
                 WHERE knowledge_binding.target_kind='KNOWLEDGE'
                   AND knowledge_binding.target_ref=a.ref
                   AND knowledge_artifact.scan_state='CLEAN'),'{}'),
       r.input,
       n.attempt,
       COALESCE((
           SELECT max(lease.generation)
           FROM control_plane.runtime_leases lease
           WHERE lease.node_id = n.id
       ), 0) + 1,
       COALESCE(t.ref, ''),
       COALESCE(a.system_key, ''),
       COALESCE((
           SELECT jsonb_agg(jsonb_build_object(
               'ref', integration_grant.ref,
               'connectionRef', connection.ref,
               'definitionKey', connection.definition_key,
               'connectionName', connection.name,
               'capabilityKey', integration_grant.capability_key,
               'capabilityName', capability.value ->> 'name',
               'capabilityDescription', capability.value ->> 'description',
               'risk', capability.value ->> 'risk'
           ) ORDER BY connection.name, integration_grant.capability_key)
           FROM control_plane.integration_grants integration_grant
           JOIN control_plane.integration_connections connection ON connection.id = integration_grant.connection_id
           JOIN control_plane.integration_definitions definition ON definition.stable_key = connection.definition_key
           JOIN LATERAL jsonb_array_elements(definition.capabilities) capability(value)
             ON capability.value ->> 'key' = integration_grant.capability_key
           WHERE integration_grant.organization_id = r.organization_id
             AND integration_grant.target_kind = 'AGENT'
             AND integration_grant.target_ref = a.ref
             AND integration_grant.enabled
             AND connection.enabled
             AND connection.state = 'CONNECTED'
           ), '[]'::jsonb),
       COALESCE((
           SELECT jsonb_agg(jsonb_build_object(
               'ref', candidate.ref,
               'name', candidate.name,
               'purpose', candidate.purpose,
               'roleDescription', candidate.role_description
           ) ORDER BY candidate.name)
           FROM control_plane.agents candidate
           WHERE candidate.organization_id = r.organization_id
             AND candidate.project_id = r.project_id
             AND candidate.id <> a.id
             AND candidate.enabled
             AND candidate.state = 'READY'
       ), '[]'::jsonb),
       COALESCE((
           SELECT edge.ref
           FROM control_plane.run_edges edge
           WHERE edge.source_node_id = n.id
             AND edge.type = 'CALLBACK_TO'
           ORDER BY edge.created_at
           LIMIT 1
       ), ''),
       COALESCE((
           SELECT jsonb_agg(jsonb_build_object('role', history.actor_kind, 'content', history.content)
                            ORDER BY history.turn_number)
           FROM (
               SELECT previous.actor_kind,
                      left(previous.content, 4000) AS content,
                      previous.turn_number
               FROM control_plane.session_turns previous
               WHERE previous.session_id = r.session_id
                 AND previous.id <> COALESCE(n.turn_id, '00000000-0000-0000-0000-000000000000'::uuid)
                 AND previous.state = 'COMPLETED'
               ORDER BY previous.turn_number DESC
               LIMIT 20
           ) history
       ), '[]'::jsonb),
       COALESCE(n.turn_id::text, ''),
       a.id::text,
       rd.id::text,
       rd.ref,
       COALESCE(role_image.recipe_id::text, ''),
       COALESCE(role_image.recipe_ref, ''),
       COALESCE(role_image.artifact_id::text, ''),
       COALESCE(role_image.artifact_ref, ''),
       COALESCE(role_image.promoted_reference, $3),
       COALESCE(role_image.manifest_digest, $4),
       COALESCE(role_image.role_runtime_contract_revision, $5),
       COALESCE(role_image.role_runtime_contract_sha256, $6)
FROM control_plane.run_nodes n
JOIN control_plane.runs r ON r.id = n.run_id
JOIN control_plane.runs root ON root.id = r.root_run_id
LEFT JOIN control_plane.projects p ON p.id = r.project_id
JOIN control_plane.sessions s ON s.id = r.session_id
JOIN control_plane.provider_accounts pa
  ON pa.id = s.provider_account_id
 AND pa.organization_id = r.organization_id
 AND pa.state = 'AUTHORIZED'
 AND pa.enabled
JOIN control_plane.provider_credential_revisions pcr
  ON pcr.id = pa.current_credential_revision_id
 AND pcr.organization_id = r.organization_id
JOIN control_plane.agents a ON a.id = n.agent_id
JOIN control_plane.role_definitions rd ON rd.id = a.role_definition_id
JOIN control_plane.runtime_profiles rp ON rp.stable_key = a.runtime_key
JOIN LATERAL (
    SELECT instruction.ref, instruction.digest, instruction.content
    FROM control_plane.instruction_versions instruction
    WHERE instruction.agent_id = a.id
      AND instruction.state = 'PUBLISHED'
    ORDER BY instruction.version_number DESC
    LIMIT 1
) iv ON true
LEFT JOIN control_plane.assistant_runtime ar ON ar.agent_id = a.id
LEFT JOIN control_plane.session_turns t ON t.id = n.turn_id
LEFT JOIN LATERAL (
    SELECT recipe.id AS recipe_id,
           recipe.ref AS recipe_ref,
           artifact.id AS artifact_id,
           artifact.ref AS artifact_ref,
           artifact.promoted_reference,
           artifact.manifest_digest,
           artifact.role_runtime_contract_revision,
           artifact.role_runtime_contract_sha256
    FROM control_plane.role_image_recipes recipe
    JOIN control_plane.image_artifacts artifact ON artifact.id = recipe.active_image_artifact_id
    WHERE recipe.organization_id = r.organization_id
      AND recipe.role_definition_id = rd.id
      AND recipe.state = 'ACTIVE'
      AND artifact.admission_state = 'ACCEPTED'
      AND artifact.promotion_state = 'PROMOTED'
      AND artifact.promoted_reference <> ''
    ORDER BY recipe.updated_at DESC, recipe.created_at DESC
    LIMIT 1
) role_image ON true
WHERE n.organization_id = $1::uuid
  AND n.type = 'AGENT_EXECUTION'
  AND n.state = 'QUEUED'
  AND r.state IN ('RUNNING', 'QUEUED')
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.run_edges edge
      JOIN control_plane.run_nodes dependency ON dependency.id = edge.source_node_id
      WHERE edge.target_node_id = n.id
        AND edge.type = 'WAITING_FOR'
        AND dependency.state <> 'SUCCEEDED'
  )
  AND (
      SELECT count(*)
      FROM control_plane.run_nodes active
      WHERE active.root_run_id = r.root_run_id
        AND active.type = 'AGENT_EXECUTION'
        AND active.state = 'RUNNING'
  ) < root.concurrency_limit
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.run_nodes earlier
      JOIN control_plane.runs earlier_run ON earlier_run.id = earlier.run_id
      WHERE earlier_run.session_id = r.session_id
        AND earlier_run.root_run_id <> r.root_run_id
        AND earlier.created_at < n.created_at
        AND earlier.type = 'AGENT_EXECUTION'
        AND earlier.state IN ('QUEUED', 'RUNNING', 'WAITING')
  )
ORDER BY CASE WHEN a.system_key = 'system-assistant' THEN 0 ELSE 1 END,
         n.created_at
FOR UPDATE OF n SKIP LOCKED
LIMIT $2;
