-- name: runtime_claimexecution_select_claimable_agent_executions :many
SELECT n.id::text,
       n.ref,
       n.run_id::text,
       r.ref,
       r.root_run_id::text,
       COALESCE(r.project_id::text, ''),
       COALESCE(p.ref, ''),
       initiator.ref,
       initiator.display_name,
       organization.ref,
       organization.name,
       COALESCE(p.name, ''),
       r.session_id::text,
       s.ref,
       COALESCE(t.content,r.task),
       r.source,
       CASE WHEN root.target_type = 'WORKFLOW' THEN root.target_ref ELSE '' END,
       COALESCE((
           SELECT schedule.ref
           FROM control_plane.schedule_occurrences occurrence
           JOIN control_plane.schedules schedule ON schedule.id = occurrence.schedule_id
           WHERE occurrence.run_id = schedule_origin.run_id
             AND occurrence.organization_id = r.organization_id
           ORDER BY occurrence.created_at DESC
           LIMIT 1
       ), ''),
       n.workflow_step_key,
       COALESCE(t.turn_number, 0),
       a.ref,
       a.name,
       a.runtime_key,
       rp.runtime_revision,
       runtime_config.provider,
       runtime_config.model,
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
       COALESCE((
           SELECT membership.role
           FROM control_plane.memberships membership
           WHERE membership.organization_id = r.organization_id
             AND membership.project_id IS NULL
             AND membership.subject_id = root.initiated_by
             AND membership.active
           LIMIT 1
       ), ''),
       COALESCE((
           SELECT membership.permissions
           FROM control_plane.memberships membership
           WHERE membership.organization_id = r.organization_id
             AND membership.project_id = r.project_id
             AND membership.subject_id = root.initiated_by
             AND membership.active
           LIMIT 1
       ), '{}'::text[]),
       CASE
           WHEN workflow_version.id IS NOT NULL
            AND n.workflow_step_key LIKE 'workflow.coordinator.%'
               THEN ARRAY['platform.run.delegate']::text[]
           ELSE COALESCE((
               SELECT ARRAY(
                   SELECT capability.value
                   FROM jsonb_array_elements_text(
                       CASE
                           WHEN jsonb_typeof(step.value -> 'RequiredCapabilityKeys') = 'array'
                               THEN step.value -> 'RequiredCapabilityKeys'
                           ELSE '[]'::jsonb
                       END
                   ) capability(value)
               )
               FROM jsonb_array_elements(
                   CASE
                       WHEN jsonb_typeof(workflow_version.spec -> 'Steps') = 'array'
                           THEN workflow_version.spec -> 'Steps'
                       ELSE '[]'::jsonb
                   END
               ) step(value)
               WHERE step.value ->> 'Key' = n.workflow_step_key
               LIMIT 1
           ), '{}'::text[])
       END,
       CASE
           WHEN NOT n.human_gate_after THEN NULL::text[]
           WHEN workflow_version.id IS NULL THEN '{}'::text[]
           ELSE COALESCE((
               SELECT ARRAY(
                   SELECT capability.value
                   FROM jsonb_array_elements_text(
                       CASE
                           WHEN jsonb_typeof(step.value -> 'RequiredCapabilityKeys') = 'array'
                               THEN step.value -> 'RequiredCapabilityKeys'
                           ELSE '[]'::jsonb
                       END
                   ) capability(value)
               )
               FROM jsonb_array_elements(
                   CASE
                       WHEN jsonb_typeof(workflow_version.spec -> 'Steps') = 'array'
                           THEN workflow_version.spec -> 'Steps'
                       ELSE '[]'::jsonb
                   END
               ) step(value)
               WHERE step.value ->> 'Key' = n.workflow_step_key
               LIMIT 1
           ), '{}'::text[])
       END,
       CASE WHEN 'platform.artifact.manage'=ANY(a.capabilities) THEN
       COALESCE((SELECT array_agg(knowledge_artifact.ref ORDER BY knowledge_binding.created_at)
                 FROM control_plane.artifact_bindings knowledge_binding
                 JOIN control_plane.artifacts knowledge_artifact ON knowledge_artifact.id=knowledge_binding.artifact_id
                 WHERE knowledge_binding.target_kind='KNOWLEDGE'
                   AND knowledge_binding.target_ref=a.ref
                   AND knowledge_artifact.organization_id=r.organization_id
                   AND knowledge_artifact.project_id=r.project_id
                   AND knowledge_artifact.scan_state='CLEAN'
                   AND knowledge_artifact.lifecycle_state='ACTIVE'),'{}')
       ELSE '{}'::text[] END,
       r.input || CASE WHEN r.source = 'SCHEDULE' THEN COALESCE((
           SELECT jsonb_build_object('automation', jsonb_build_object(
               'occurrenceRef', occurrence.ref, 'attempt', occurrence.attempt,
               'scheduleRef', schedule.ref, 'scheduleRevisionRef', revision.ref,
               'scheduleRevision', revision.revision, 'scheduleRevisionDigest', revision.digest,
               'name',revision.name,'timezone',revision.timezone,'scheduledAt',occurrence.scheduled_for,
               'targetRef', occurrence.target_ref, 'targetVersion', occurrence.target_version,
               'targetDigest', occurrence.target_digest, 'text', occurrence.automation_text,
               'textDigest', occurrence.automation_text_digest, 'promptInputs', occurrence.prompt_inputs,
               'promptInputsDigest', occurrence.prompt_inputs_digest, 'promptInputFormat', occurrence.prompt_input_format,
               'promptInputsRaw', occurrence.prompt_inputs::text))
           FROM control_plane.schedule_occurrences occurrence
           JOIN control_plane.schedules schedule ON schedule.id = occurrence.schedule_id
           JOIN control_plane.schedule_revisions revision ON revision.id = occurrence.schedule_revision_id
           WHERE occurrence.run_id = schedule_origin.run_id AND occurrence.organization_id = r.organization_id
       ), '{}'::jsonb) ELSE '{}'::jsonb END,
       COALESCE(input_attachment_set.ref, ''),
       COALESCE(input_attachment_set.manifest_digest, ''),
       COALESCE(input_attachment_set.purpose, ''),
       CASE WHEN 'platform.artifact.manage'=ANY(a.capabilities) THEN COALESCE((
           SELECT jsonb_agg(jsonb_build_object(
               'ref', runtime_set.ref,
               'manifestDigest', runtime_set.manifest_digest,
               'purpose', runtime_set.purpose,
               'scope', runtime_set.usage_scope,
               'provenance', runtime_set.provenance,
               'turnRef', runtime_set.turn_ref
           ) ORDER BY runtime_set.scope_order, runtime_set.turn_number, runtime_set.ref)
           FROM (
               SELECT input_attachment_set.ref,
                      input_attachment_set.manifest_digest,
                      input_attachment_set.purpose,
                      'INPUT'::text AS usage_scope,
                      'CURRENT_TURN'::text AS provenance,
                      COALESCE(t.ref, '') AS turn_ref,
                      0::integer AS scope_order,
                      COALESCE(t.turn_number, 0)::bigint AS turn_number
               WHERE input_attachment_set.id IS NOT NULL
                 AND input_attachment_set.state = 'FINALIZED'

               UNION ALL

               SELECT previous_set.ref,
                      previous_set.manifest_digest,
                      previous_set.purpose,
                      'SESSION'::text,
                      'SESSION_HISTORY'::text,
                      previous_turn.ref,
                      1::integer,
                      previous_turn.turn_number
               FROM LATERAL (
                   SELECT DISTINCT ON (candidate.attachment_set_id)
                          candidate.id,
                          candidate.ref,
                          candidate.turn_number,
                          candidate.attachment_set_id
                   FROM control_plane.session_turns AS candidate
                   WHERE candidate.session_id = s.id
                     AND candidate.attachment_set_id IS NOT NULL
                     AND (t.id IS NULL OR candidate.id <> t.id)
                   ORDER BY candidate.attachment_set_id, candidate.turn_number
               ) AS previous_turn
               JOIN control_plane.attachment_sets AS previous_set
                 ON previous_set.id = previous_turn.attachment_set_id
                AND previous_set.state = 'FINALIZED'
               WHERE (input_attachment_set.id IS NULL OR previous_set.id <> input_attachment_set.id)
                 AND previous_set.item_count = (
                     SELECT count(*)
                     FROM control_plane.attachment_set_items AS eligible_item
                     JOIN control_plane.artifacts AS eligible_artifact
                       ON eligible_artifact.id = eligible_item.artifact_id
                     JOIN control_plane.artifact_content AS eligible_content
                       ON eligible_content.artifact_id = eligible_artifact.id
                     WHERE eligible_item.attachment_set_id = previous_set.id
                       AND eligible_artifact.scan_state = 'CLEAN'
                       AND (
                           eligible_artifact.lifecycle_state = 'ACTIVE'
                           OR (
                               eligible_artifact.lifecycle_state = 'DELETED'
                               AND eligible_artifact.deleted_at > r.created_at
                           )
                       )
                       AND eligible_artifact.ref = eligible_item.artifact_ref
                       AND eligible_artifact.revision = eligible_item.artifact_revision
                       AND eligible_artifact.file_name = eligible_item.file_name
                       AND eligible_artifact.media_type = eligible_item.media_type
                       AND eligible_artifact.size_bytes = eligible_item.size_bytes
                       AND eligible_artifact.digest = eligible_item.digest
                       AND eligible_artifact.source = eligible_item.source
                       AND eligible_content.digest = eligible_item.digest
                       AND eligible_content.size_bytes = eligible_item.size_bytes
                 )
           ) AS runtime_set
       ), '[]'::jsonb) ELSE '[]'::jsonb END,
       CASE WHEN 'platform.artifact.manage'=ANY(a.capabilities) THEN COALESCE((
           SELECT jsonb_agg(jsonb_build_object(
               'ref', runtime_artifact.ref,
               'fileName', runtime_artifact.file_name,
               'mediaType', runtime_artifact.media_type,
               'sizeBytes', runtime_artifact.size_bytes,
               'digest', runtime_artifact.digest,
               'revision', runtime_artifact.revision,
               'version', runtime_artifact.version,
               'source', runtime_artifact.source,
               'scope', runtime_artifact.usage_scope,
               'position', runtime_artifact.position,
               'attachmentSetRef', runtime_artifact.attachment_set_ref,
               'attachmentPurpose', runtime_artifact.attachment_purpose,
               'provenance', runtime_artifact.provenance
           ) ORDER BY runtime_artifact.scope_order,
                      runtime_artifact.set_order,
                      runtime_artifact.position,
                      runtime_artifact.file_name,
                      runtime_artifact.ref)
           FROM (
                   SELECT item.artifact_ref AS ref,
                          item.file_name,
                          item.media_type,
                          item.size_bytes,
                          item.digest,
                          item.artifact_revision AS revision,
                          item.artifact_version AS version,
                          item.source,
                          'INPUT'::text AS usage_scope,
                          item.position,
                          0::integer AS scope_order,
                          0::bigint AS set_order,
                          input_attachment_set.ref AS attachment_set_ref,
                          input_attachment_set.purpose AS attachment_purpose,
                          'CURRENT_TURN'::text AS provenance
                   FROM control_plane.attachment_set_items AS item
                   JOIN control_plane.artifacts AS artifact ON artifact.id = item.artifact_id
                   JOIN control_plane.artifact_content AS content ON content.artifact_id = artifact.id
                   WHERE item.attachment_set_id = input_attachment_set.id
                     AND artifact.scan_state = 'CLEAN'
                     AND artifact.lifecycle_state IN ('ACTIVE', 'DELETED')
                     AND artifact.ref = item.artifact_ref
                     AND artifact.revision = item.artifact_revision
                     AND artifact.file_name = item.file_name
                     AND artifact.media_type = item.media_type
                     AND artifact.size_bytes = item.size_bytes
                     AND artifact.digest = item.digest
                     AND artifact.source = item.source
                     AND content.digest = item.digest
                     AND content.size_bytes = item.size_bytes
                     AND input_attachment_set.state = 'FINALIZED'

                   UNION ALL

                   SELECT previous_item.artifact_ref,
                          previous_item.file_name,
                          previous_item.media_type,
                          previous_item.size_bytes,
                          previous_item.digest,
                          previous_item.artifact_revision,
                          previous_item.artifact_version,
                          previous_item.source,
                          'SESSION'::text AS usage_scope,
                          previous_item.position,
                          1::integer AS scope_order,
                          previous_turn.turn_number AS set_order,
                          previous_set.ref AS attachment_set_ref,
                          previous_set.purpose AS attachment_purpose,
                          'SESSION_HISTORY'::text AS provenance
                   FROM LATERAL (
                       SELECT DISTINCT ON (candidate.attachment_set_id)
                              candidate.id,
                              candidate.ref,
                              candidate.turn_number,
                              candidate.attachment_set_id
                       FROM control_plane.session_turns AS candidate
                       WHERE candidate.session_id = s.id
                         AND candidate.attachment_set_id IS NOT NULL
                         AND (t.id IS NULL OR candidate.id <> t.id)
                       ORDER BY candidate.attachment_set_id, candidate.turn_number
                   ) AS previous_turn
                   JOIN control_plane.attachment_sets AS previous_set
                     ON previous_set.id = previous_turn.attachment_set_id
                    AND previous_set.state = 'FINALIZED'
                   JOIN control_plane.attachment_set_items AS previous_item
                     ON previous_item.attachment_set_id = previous_set.id
                   JOIN control_plane.artifacts AS previous_artifact
                     ON previous_artifact.id = previous_item.artifact_id
                   JOIN control_plane.artifact_content AS previous_content
                     ON previous_content.artifact_id = previous_artifact.id
                   WHERE (input_attachment_set.id IS NULL OR previous_set.id <> input_attachment_set.id)
                     AND previous_artifact.scan_state = 'CLEAN'
                     AND (
                         previous_artifact.lifecycle_state = 'ACTIVE'
                         OR (
                             previous_artifact.lifecycle_state = 'DELETED'
                             AND previous_artifact.deleted_at > r.created_at
                         )
                     )
                     AND previous_artifact.ref = previous_item.artifact_ref
                     AND previous_artifact.revision = previous_item.artifact_revision
                     AND previous_artifact.file_name = previous_item.file_name
                     AND previous_artifact.media_type = previous_item.media_type
                     AND previous_artifact.size_bytes = previous_item.size_bytes
                     AND previous_artifact.digest = previous_item.digest
                     AND previous_artifact.source = previous_item.source
                     AND previous_content.digest = previous_item.digest
                     AND previous_content.size_bytes = previous_item.size_bytes
                     AND previous_set.item_count = (
                         SELECT count(*)
                         FROM control_plane.attachment_set_items AS eligible_item
                         JOIN control_plane.artifacts AS eligible_artifact
                           ON eligible_artifact.id = eligible_item.artifact_id
                         JOIN control_plane.artifact_content AS eligible_content
                           ON eligible_content.artifact_id = eligible_artifact.id
                         WHERE eligible_item.attachment_set_id = previous_set.id
                           AND eligible_artifact.scan_state = 'CLEAN'
                           AND (
                               eligible_artifact.lifecycle_state = 'ACTIVE'
                               OR (
                                   eligible_artifact.lifecycle_state = 'DELETED'
                                   AND eligible_artifact.deleted_at > r.created_at
                               )
                           )
                           AND eligible_artifact.ref = eligible_item.artifact_ref
                           AND eligible_artifact.revision = eligible_item.artifact_revision
                           AND eligible_artifact.file_name = eligible_item.file_name
                           AND eligible_artifact.media_type = eligible_item.media_type
                           AND eligible_artifact.size_bytes = eligible_item.size_bytes
                           AND eligible_artifact.digest = eligible_item.digest
                           AND eligible_artifact.source = eligible_item.source
                           AND eligible_content.digest = eligible_item.digest
                           AND eligible_content.size_bytes = eligible_item.size_bytes
                     )

                   UNION ALL

                   SELECT knowledge_artifact.ref,
                          knowledge_artifact.file_name,
                          knowledge_artifact.media_type,
                          knowledge_artifact.size_bytes,
                          knowledge_artifact.digest,
                          knowledge_artifact.revision,
                          knowledge_artifact.version,
                          knowledge_artifact.source,
                          'KNOWLEDGE'::text AS usage_scope,
                          row_number() OVER (ORDER BY knowledge_binding.created_at, knowledge_artifact.ref),
                          2::integer AS scope_order,
                          0::bigint AS set_order,
                          ''::text AS attachment_set_ref,
                          'PROJECT_KNOWLEDGE'::text AS attachment_purpose,
                          'PROJECT_BINDING'::text AS provenance
                   FROM control_plane.artifact_bindings AS knowledge_binding
                   JOIN control_plane.artifacts AS knowledge_artifact ON knowledge_artifact.id = knowledge_binding.artifact_id
                   JOIN control_plane.artifact_content AS knowledge_content ON knowledge_content.artifact_id = knowledge_artifact.id
                   WHERE knowledge_binding.target_kind = 'KNOWLEDGE'
                     AND knowledge_binding.target_ref = a.ref
                     AND knowledge_artifact.organization_id = r.organization_id
                     AND knowledge_artifact.project_id = r.project_id
                     AND knowledge_artifact.scan_state = 'CLEAN'
                     AND knowledge_artifact.lifecycle_state = 'ACTIVE'
           ) AS runtime_artifact
       ), '[]'::jsonb) ELSE '[]'::jsonb END,
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
		       'grantVersion', integration_grant.version::text,
               'connectionRef', connection.ref,
               'definitionKey', connection.definition_key,
               'definitionVersion', connection.definition_version,
               'definitionDigest', connection.definition_digest,
               'connectionName', connection.name,
               'capabilityKey', integration_grant.capability_key,
               'capabilityName', capability.value ->> 'name',
               'capabilityDescription', capability.value ->> 'description',
               'operation', capability.value ->> 'operation',
               'inputSchema', capability.value ->> 'inputSchema',
               'inputSchemaSha256', capability.value ->> 'inputSchemaSha256',
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
             AND integration_grant.definition_version=connection.definition_version
             AND integration_grant.definition_digest=connection.definition_digest
             AND definition.enabled
             AND (definition.adapter_owner,definition.execution_route) IN
                 (('integration-gateway','MANAGED_MCP'),('interaction-gateway','INTERACTION'))
             AND definition.adapter_readiness = 'READY'
             AND capability.value->>'operation' NOT IN ('mattermost.inbound','mattermost.gate_decisions')
             AND connection.enabled
             AND connection.lifecycle_state='ACTIVE'
             AND connection.state = 'CONNECTED'
           ), '[]'::jsonb),
           CASE
               WHEN a.system_key <> 'system-assistant'
                AND 'platform.run.delegate' <> ALL(a.capabilities) THEN '[]'::jsonb
               ELSE COALESCE((
                   SELECT jsonb_agg(jsonb_build_object(
                       'ref', target.ref,
                       'name', target.name,
                       'purpose', target.purpose,
                       'roleDescription', target.role_description,
                       'workflowStepKey', target.workflow_step_key,
                       'workflowStepName', target.workflow_step_name,
                       'instructions', target.instructions,
                       'expectedResult', target.expected_result
                   ) ORDER BY target.position, target.name)
                   FROM (
                       SELECT candidate.ref,
                              candidate.name,
                              candidate.purpose,
                              candidate.role_description,
                              ''::text AS workflow_step_key,
                              ''::text AS workflow_step_name,
                              ''::text AS instructions,
                              ''::text AS expected_result,
                              0::bigint AS position
                       FROM control_plane.agents candidate
                       WHERE root.workflow_version_id IS NULL
                         AND NOT EXISTS (
                             SELECT 1
                             FROM control_plane.run_edges continuation
                             WHERE continuation.root_run_id = root.id
                               AND continuation.target_node_id = n.id
                               AND continuation.type = 'CONTINUES'
                         )
                         AND candidate.organization_id = r.organization_id
                         AND candidate.project_id = r.project_id
                         AND candidate.id <> a.id
                         AND candidate.enabled
                         AND candidate.state = 'READY'

                       UNION ALL

                       SELECT candidate.ref,
                              candidate.name,
                              candidate.purpose,
                              candidate.role_description,
                              step.value ->> 'Key',
                              step.value ->> 'Name',
                              step.value ->> 'Instructions',
                              COALESCE(step.value ->> 'ExpectedResult', ''),
                              step.position
                       FROM jsonb_array_elements(COALESCE(workflow_version.spec -> 'Steps', '[]'::jsonb))
                            WITH ORDINALITY AS step(value, position)
                       JOIN control_plane.agents candidate
                         ON candidate.organization_id = r.organization_id
                        AND candidate.project_id = r.project_id
                        AND candidate.ref = step.value ->> 'AgentRef'
                        AND candidate.id <> a.id
                        AND candidate.enabled
                        AND candidate.state = 'READY'
                       WHERE root.workflow_version_id IS NOT NULL
                         AND a.ref = workflow_version.spec ->> 'CoordinatorAgentRef'
                         AND NOT EXISTS (
                             SELECT 1
                             FROM control_plane.run_nodes delegated
                             WHERE delegated.root_run_id = root.id
                               AND delegated.workflow_step_key = step.value ->> 'Key'
                               AND delegated.materialization_state = 'MATERIALIZED'
                         )
                   ) target
               ), '[]'::jsonb)
           END,
       COALESCE((
           SELECT edge.ref
           FROM control_plane.run_edges edge
           WHERE edge.source_node_id = n.id
             AND edge.type = 'CALLBACK_TO'
           ORDER BY edge.created_at
           LIMIT 1
       ), ''),
       COALESCE((
           SELECT jsonb_agg(jsonb_build_object(
                                'role', CASE
                                    WHEN history.actor_kind = 'USER' THEN 'USER'
                                    ELSE 'ASSISTANT'
                                END,
                                'content', history.content
                            )
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
       COALESCE(role_image.recipe_generation, 0),
       CASE WHEN a.system_key = 'system-assistant' THEN $3
            ELSE COALESCE(role_image.promoted_reference, '') END,
       CASE WHEN a.system_key = 'system-assistant' THEN $4
            ELSE COALESCE(role_image.manifest_digest, '') END,
       CASE WHEN a.system_key = 'system-assistant' THEN $5
            ELSE COALESCE(role_image.role_runtime_contract_revision, 0) END,
       CASE WHEN a.system_key = 'system-assistant' THEN $6
            ELSE COALESCE(role_image.role_runtime_contract_sha256, '') END,
       runtime_config.id::text,
       runtime_config.ref,
       runtime_config.version_number,
       runtime_config.digest,
       provider_policy.id::text,
       provider_policy.ref,
       provider_policy.version_number,
       provider_policy.digest,
       provider_policy.mode,
       config_overlay.id::text,
       config_overlay.ref,
       config_overlay.version_number,
       config_overlay.digest,
       config_overlay.content,
       environment_binding.id::text,
       environment_binding.ref,
       environment_binding.version,
       environment_binding.digest,
       runtime_environment.id::text,
       environment_set.ref,
       runtime_environment.version_number,
       runtime_environment.digest,
       runtime_environment.non_secret_values,
       runtime_environment.secret_descriptors,
       runtime_environment.selected_tools,
       runtime_environment.resource_policy,
       runtime_environment.volume_policy,
       runtime_environment.network_policy,
       runtime_environment.kubernetes_access_profile,
       runtime_environment.resources_digest,
       runtime_environment.volumes_digest,
       runtime_environment.network_digest,
       runtime_environment.rbac_digest,
       COALESCE(session_storage.codex_session_id::text, ''),
       COALESCE(storage_revision.safe_snapshot #>> '{contextSnapshot,digest}', '')
FROM control_plane.run_nodes n
JOIN control_plane.runs r ON r.id = n.run_id
JOIN control_plane.runs root ON root.id = r.root_run_id
LEFT JOIN LATERAL (
    -- Повтор Run наследует происхождение только по серверному ребру, не из input.
    WITH RECURSIVE ancestors AS (
        SELECT root.id, root.retry_of_run_id
        WHERE root.source = 'SCHEDULE'
        UNION
        SELECT previous.id, previous.retry_of_run_id
        FROM ancestors current_run
        JOIN control_plane.runs previous ON previous.id = current_run.retry_of_run_id
        WHERE previous.organization_id = root.organization_id
    )
    SELECT occurrence.run_id
    FROM ancestors
    JOIN control_plane.schedule_occurrences occurrence ON occurrence.run_id = ancestors.id
    WHERE occurrence.organization_id = root.organization_id
    LIMIT 1
) schedule_origin ON true
JOIN control_plane.subjects initiator ON initiator.id = root.initiated_by
JOIN control_plane.organizations organization ON organization.id = r.organization_id
LEFT JOIN control_plane.projects p ON p.id = r.project_id
JOIN control_plane.sessions s ON s.id = r.session_id
LEFT JOIN control_plane.session_storage session_storage ON session_storage.session_id = s.id
LEFT JOIN control_plane.runtime_revisions storage_revision
  ON storage_revision.id = session_storage.runtime_revision_id
 AND storage_revision.organization_id = r.organization_id
 AND storage_revision.session_id = s.id
JOIN control_plane.provider_accounts pa
  ON pa.id = s.provider_account_id
 AND pa.organization_id = r.organization_id
 AND pa.state = 'AUTHORIZED'
 AND pa.enabled
JOIN control_plane.provider_credential_revisions pcr
  ON pcr.id = pa.current_credential_revision_id
 AND pcr.organization_id = r.organization_id
JOIN control_plane.agents a ON a.id = n.agent_id
JOIN control_plane.agent_runtime_config_versions runtime_config ON runtime_config.id = a.current_runtime_config_id
JOIN control_plane.provider_account_policy_versions provider_policy ON provider_policy.id = runtime_config.provider_account_policy_id
JOIN control_plane.agent_config_overlay_versions config_overlay ON config_overlay.id = a.current_config_overlay_id AND config_overlay.state = 'PUBLISHED'
JOIN control_plane.agent_runtime_environment_bindings environment_binding ON environment_binding.agent_id = a.id
JOIN control_plane.runtime_environment_sets environment_set ON environment_set.id = environment_binding.environment_set_id AND environment_set.state = 'ACTIVE'
JOIN control_plane.runtime_environment_versions runtime_environment ON runtime_environment.id =
    CASE WHEN a.project_id IS NULL AND a.system_key = 'system-assistant'
         THEN environment_set.current_version_id ELSE environment_binding.environment_version_id END
JOIN control_plane.role_definitions rd ON rd.id = a.role_definition_id
JOIN control_plane.runtime_profiles rp ON rp.stable_key = runtime_config.runtime_profile_key
  AND rp.provider = runtime_config.provider
JOIN control_plane.provider_definitions runtime_provider_definition ON runtime_provider_definition.stable_key = runtime_config.provider
  AND runtime_provider_definition.stable_key = pa.definition_key
LEFT JOIN control_plane.workflow_versions workflow_version
  ON workflow_version.id = root.workflow_version_id
JOIN LATERAL (
    SELECT source.ref, source.digest, source.content
    FROM (
        SELECT revision.ref, revision.digest, revision.content, revision.revision, 1 AS priority
        FROM control_plane.managed_configuration_bindings binding
        JOIN control_plane.managed_configuration_sets configuration
          ON configuration.id = binding.configuration_set_id
         AND configuration.kind = 'PROMPT_TEMPLATE'
         AND configuration.organization_id = a.organization_id
         AND configuration.project_id = a.project_id
        JOIN control_plane.managed_configuration_revisions revision
          ON revision.id = binding.configuration_revision_id
         AND revision.configuration_set_id = configuration.id
         AND revision.state IN ('PUBLISHED', 'SUPERSEDED')
        WHERE binding.organization_id = a.organization_id
          AND binding.project_id = a.project_id
          AND binding.configuration_kind = 'PROMPT_TEMPLATE'
          AND binding.consumer_kind = 'AGENT'
          AND binding.consumer_ref = a.ref

        UNION ALL

        SELECT instruction.ref, instruction.digest, instruction.content,
               instruction.version_number::bigint, 2
        FROM control_plane.instruction_versions instruction
        JOIN control_plane.agent_instruction_bindings active_instruction
          ON active_instruction.instruction_id=instruction.id AND active_instruction.agent_id=a.id
         AND active_instruction.organization_id=a.organization_id
        WHERE instruction.agent_id = a.id
          AND instruction.state = 'PUBLISHED'
    ) source
    ORDER BY source.priority, source.revision DESC
    LIMIT 1
) iv ON true
LEFT JOIN control_plane.assistant_runtime ar ON ar.agent_id = a.id
LEFT JOIN control_plane.session_turns t ON t.id = n.turn_id
LEFT JOIN control_plane.attachment_sets input_attachment_set
  ON input_attachment_set.id = COALESCE(t.attachment_set_id, root.input_attachment_set_id)
LEFT JOIN LATERAL (
    SELECT recipe.id AS recipe_id,
           recipe.ref AS recipe_ref,
           artifact.id AS artifact_id,
           artifact.ref AS artifact_ref,
           artifact.recipe_generation,
           artifact.promoted_reference,
           artifact.manifest_digest,
           artifact.role_runtime_contract_revision,
           artifact.role_runtime_contract_sha256
    FROM control_plane.image_artifacts artifact
    JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
    WHERE artifact.id = runtime_environment.role_image_artifact_id
      AND artifact.organization_id = r.organization_id
      AND recipe.project_id = r.project_id
      AND recipe.state = 'ACTIVE'
      AND artifact.admission_state = 'ACCEPTED'
      AND artifact.promotion_state = 'PROMOTED'
      AND artifact.promoted_reference <> ''
      AND artifact.role_runtime_contract_revision = $5
      AND artifact.role_runtime_contract_sha256 = $6
    LIMIT 1
) role_image ON true
WHERE n.organization_id = $1::uuid
  AND n.type = 'AGENT_EXECUTION'
  AND n.state = 'QUEUED'
  AND r.state IN ('RUNNING', 'QUEUED')
  AND COALESCE(session_storage.state, 'LIVE') = 'LIVE'
  AND (
      (a.system_key = 'system-assistant' AND runtime_environment.role_image_artifact_id IS NULL)
      OR
      (a.system_key IS NULL AND runtime_environment.role_image_artifact_id IS NOT NULL AND role_image.artifact_id IS NOT NULL)
  )
  AND (
      input_attachment_set.id IS NULL
      OR input_attachment_set.item_count = (
          SELECT count(*)
          FROM control_plane.attachment_set_items AS input_item
          JOIN control_plane.artifacts AS input_artifact ON input_artifact.id = input_item.artifact_id
          JOIN control_plane.artifact_content AS input_content ON input_content.artifact_id = input_artifact.id
          WHERE input_item.attachment_set_id = input_attachment_set.id
            AND input_artifact.scan_state = 'CLEAN'
            AND input_artifact.lifecycle_state IN ('ACTIVE', 'DELETED')
            AND input_artifact.ref = input_item.artifact_ref
            AND input_artifact.revision = input_item.artifact_revision
            AND input_artifact.file_name = input_item.file_name
            AND input_artifact.media_type = input_item.media_type
            AND input_artifact.size_bytes = input_item.size_bytes
            AND input_artifact.digest = input_item.digest
            AND input_artifact.source = input_item.source
            AND input_content.digest = input_item.digest
            AND input_content.size_bytes = input_item.size_bytes
      )
  )
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
