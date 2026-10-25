-- name: runtime_callback_resolve_continuation :one
SELECT parent_node.run_id::text,
       parent_node.root_run_id::text,
       parent_node.agent_id::text,
       parent_node.attempt,
       parent_node.display_name,
       parent_node.role,
       parent_run.session_id::text,
       parent_agent.ref,
       COALESCE(root.workflow_version_id::text, ''),
       parent_node.workflow_step_key,
       parent_node.human_gate_after,
       EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_node_id=parent_node.id),
       parent_node.ref,
       jsonb_build_object(
           'completedChildren', COALESCE((
               SELECT jsonb_agg(jsonb_build_object(
                   'runRef', child.ref,
                   'nodeRef', child_node.ref,
                   'state', receipt.result_snapshot->>'state',
                   'resultSummary', receipt.result_snapshot->>'resultSummary',
                   'artifactRefs', COALESCE((
                       SELECT jsonb_agg(pin.value->>'ref' ORDER BY pin.value->>'ref')
                       FROM jsonb_array_elements(receipt.result_snapshot->'artifacts') pin(value)
                   ), '[]'::jsonb)
               ) || CASE WHEN EXISTS (SELECT 1 FROM control_plane.repository_bundle_publications publication
                   WHERE publication.organization_id=child.organization_id AND publication.origin_run_id=child.id
                     AND publication.root_run_id=callback_edge.root_run_id AND publication.project_id=child.project_id)
                 THEN jsonb_build_object('repositoryPublicationOutcome',control_plane.repository_bundle_callback_outcome(child.organization_id,child.id))
                 ELSE '{}'::jsonb END ORDER BY callback_edge.created_at, callback_edge.ref)
               FROM control_plane.run_edges callback_edge
               JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id = callback_edge.id
               JOIN control_plane.run_nodes child_node ON child_node.id = callback_edge.source_node_id
               JOIN control_plane.runs child ON child.id = receipt.child_run_id
               WHERE callback_edge.organization_id = parent_node.organization_id
                 AND callback_edge.root_run_id = root.id
                 AND callback_edge.target_node_id = parent_node.id
                 AND callback_edge.type = 'CALLBACK_TO'
                 AND child_node.organization_id = parent_node.organization_id
                 AND child.organization_id = parent_node.organization_id
                 AND (
                     child_node.run_id = child.id
                     OR EXISTS (
                         SELECT 1 FROM control_plane.required_workflow_launches launch
                         WHERE launch.organization_id = parent_node.organization_id
                           AND launch.origin_root_run_id = root.id
                           AND launch.origin_node_id = parent_node.id
                           AND launch.proxy_node_id = child_node.id
                           AND launch.callback_edge_id = callback_edge.id
                           AND launch.child_root_run_id = child.id
                           AND launch.state IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
                     )
                 )
                 AND receipt.result_snapshot->>'state' IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
           ), '[]'::jsonb),
           'remainingStepKeys', COALESCE((
               SELECT jsonb_agg(step.value ->> 'Key' ORDER BY step.position)
               FROM jsonb_array_elements(COALESCE(workflow_version.spec -> 'Steps', '[]'::jsonb))
                    WITH ORDINALITY AS step(value, position)
               WHERE NOT EXISTS (
                   SELECT 1 FROM control_plane.run_nodes delegated
                   WHERE delegated.organization_id = parent_node.organization_id
                     AND delegated.root_run_id = root.id
                     AND delegated.workflow_step_key = step.value ->> 'Key'
                     AND delegated.materialization_state = 'MATERIALIZED'
               )
           ), '[]'::jsonb)
       ) || CASE WHEN own_publication.id IS NULL THEN '{}'::jsonb ELSE jsonb_build_object(
           'repositoryPublicationOutcome',control_plane.repository_bundle_callback_outcome(parent_node.organization_id,parent_run.id),
           'repositoryPublicationIntegration',jsonb_build_object(
               'invocation_ref',own_invocation.ref,'state',own_invocation.state,
               'result_summary',left(COALESCE(own_invocation.result_summary,''),4000),
               'safe_error_code',COALESCE(own_invocation.safe_error_code,''),
               'owner_decision',jsonb_build_object('gate_ref',own_gate.ref,'gate_version',own_gate.version,
                   'decision',COALESCE(own_gate.decision,''),'comment',own_gate.decision_comment))) END,
       own_publication.id IS NOT NULL
FROM control_plane.run_nodes parent_node
JOIN control_plane.runs parent_run ON parent_run.id = parent_node.run_id
JOIN control_plane.runs root ON root.id = parent_node.root_run_id
JOIN control_plane.agents parent_agent ON parent_agent.id = parent_node.agent_id
LEFT JOIN control_plane.repository_bundle_publications own_publication
  ON own_publication.organization_id=parent_node.organization_id AND own_publication.origin_node_id=parent_node.id
 AND own_publication.origin_run_id=parent_run.id AND own_publication.root_run_id=root.id
 AND own_publication.project_id=parent_run.project_id
LEFT JOIN control_plane.integration_invocations own_invocation
  ON own_invocation.id=own_publication.integration_invocation_id AND own_invocation.organization_id=parent_node.organization_id
 AND own_invocation.operation='github.repository.bundle.publish' AND own_invocation.node_id=parent_node.id AND own_invocation.run_id=parent_run.id
LEFT JOIN control_plane.owner_gates own_gate
  ON own_gate.id=own_publication.owner_gate_id AND own_gate.organization_id=parent_node.organization_id
 AND own_gate.integration_invocation_id=own_invocation.id AND own_gate.root_run_id=root.id
 AND own_gate.project_id=parent_run.project_id AND own_gate.scope_kind='PROJECT'
LEFT JOIN control_plane.workflow_versions workflow_version
  ON workflow_version.id = root.workflow_version_id
WHERE parent_node.organization_id = @organization_id::uuid
  AND parent_node.id = @parent_node_id::uuid
  AND parent_node.state = 'SUCCEEDED'
  AND NOT EXISTS (
      SELECT 1 FROM control_plane.required_workflow_launches launch
      WHERE launch.origin_node_id=parent_node.id AND launch.state='OPEN'
  )
  AND root.state IN ('RUNNING', 'WAITING_HUMAN')
  AND (own_publication.id IS NULL OR (
      own_publication.state IN ('SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED')
      AND ((own_gate.state='APPROVED' AND own_invocation.state IN ('SUCCEEDED','FAILED'))
        OR (own_gate.state='REJECTED' AND own_invocation.state='REJECTED')
        OR (own_gate.state='CANCELLED' AND own_invocation.state='CANCELLED'))
  ))
  AND ((own_publication.id IS NULL AND NOT EXISTS (
      SELECT 1 FROM control_plane.run_edges callback
      JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=callback.id AND receipt.organization_id=parent_node.organization_id
      JOIN control_plane.repository_bundle_publications received_publication ON received_publication.organization_id=parent_node.organization_id
        AND received_publication.root_run_id=root.id AND received_publication.origin_run_id=receipt.child_run_id
      WHERE callback.organization_id=parent_node.organization_id AND callback.root_run_id=root.id
        AND callback.target_node_id=parent_node.id AND callback.type='CALLBACK_TO')) OR (
      parent_agent.enabled AND parent_agent.state='READY'
      AND EXISTS (SELECT 1 FROM control_plane.projects project WHERE project.id=parent_run.project_id AND project.lifecycle='ACTIVE')
      AND EXISTS (SELECT 1 FROM control_plane.subjects initiator
        JOIN control_plane.catalog_access_targets target ON target.organization_id=initiator.organization_id AND target.kind='AGENT' AND target.id=parent_agent.id
        WHERE initiator.id=root.initiated_by AND initiator.organization_id=parent_node.organization_id AND initiator.active
          AND control_plane.catalog_resource_visible(parent_node.organization_id,initiator.id,'agent.launch','AGENT',target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false))
  ))
  AND (
      NOT EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_node_id=parent_node.id)
      OR EXISTS(
          SELECT 1 FROM control_plane.required_workflow_launches launch
          JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=launch.callback_edge_id AND receipt.child_run_id=launch.child_root_run_id
          JOIN control_plane.runs child ON child.id=launch.child_root_run_id
          JOIN control_plane.session_turns delivered ON delivered.run_id=parent_run.id AND delivered.session_id=parent_run.session_id
             AND delivered.actor_kind='AGENT' AND delivered.actor_ref=child.ref AND delivered.state='COMPLETED'
          JOIN control_plane.session_turns current_turn ON current_turn.id=parent_node.turn_id
          WHERE launch.origin_node_id=parent_node.id AND launch.organization_id=parent_node.organization_id
            AND launch.state<>'OPEN' AND delivered.turn_number>current_turn.turn_number
      )
  )
  AND (
      EXISTS (
      SELECT 1
      FROM control_plane.run_edges callback_edge
      WHERE callback_edge.root_run_id = root.id
        AND callback_edge.target_node_id = parent_node.id
        AND callback_edge.type = 'CALLBACK_TO'
      )
  )
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.run_edges callback_edge
      WHERE callback_edge.root_run_id = root.id
        AND callback_edge.target_node_id = parent_node.id
        AND callback_edge.type = 'CALLBACK_TO'
        AND NOT EXISTS (
            SELECT 1
            FROM control_plane.callback_receipts receipt
            WHERE receipt.callback_edge_id = callback_edge.id
        )
  )
  AND (
      root.workflow_version_id IS NULL
      OR workflow_version.spec ->> 'CoordinatorAgentRef' = parent_agent.ref
      OR EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch
                WHERE launch.origin_node_id=parent_node.id AND launch.origin_root_run_id=root.id
                  AND launch.origin_run_id=parent_run.id AND launch.organization_id=parent_node.organization_id)
  )
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.run_edges continuation
      WHERE continuation.root_run_id = root.id
        AND continuation.source_node_id = parent_node.id
        AND continuation.type = 'CONTINUES'
  )
FOR UPDATE OF parent_node
