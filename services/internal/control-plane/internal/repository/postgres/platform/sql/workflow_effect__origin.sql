-- name: workflow_effect__origin :one
SELECT anchor.id::text,node.id::text,node.run_id::text,revision.id::text,lease.id::text,
       revision.generation,node.attempt,node.workflow_business_attempt,step.value,node.workflow_inputs,
       anchor.workflow_effect_intents,node.workflow_effect_rejoins,connection.version,current_session.ref,current_turn.ref,
       (SELECT COALESCE(sum(jsonb_array_length(sibling.workflow_effect_rejoins)),0)::integer
        FROM control_plane.run_nodes sibling WHERE sibling.organization_id=node.organization_id AND sibling.root_run_id=node.root_run_id
            AND COALESCE(sibling.workflow_step_origin_node_id,sibling.id)=anchor.id
            AND sibling.workflow_business_attempt=node.workflow_business_attempt)
FROM control_plane.runs root
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id AND workflow.project_id=root.project_id
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
JOIN control_plane.run_nodes node ON node.root_run_id=root.id AND node.organization_id=root.organization_id
JOIN control_plane.run_nodes anchor ON anchor.id=COALESCE(node.workflow_step_origin_node_id,node.id)
    AND anchor.organization_id=node.organization_id AND anchor.root_run_id=root.id AND anchor.agent_id=node.agent_id
    AND anchor.workflow_step_key=node.workflow_step_key AND anchor.workflow_step_origin_node_id IS NULL AND anchor.workflow_business_attempt=1
JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=root.organization_id
    AND physical.root_run_id=root.id AND physical.project_id=root.project_id
JOIN control_plane.sessions current_session ON current_session.id=physical.session_id AND current_session.organization_id=physical.organization_id
JOIN control_plane.session_turns current_turn ON current_turn.id=node.turn_id AND current_turn.run_id=physical.id AND current_turn.session_id=current_session.id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=root.organization_id AND agent.project_id=root.project_id AND agent.enabled AND agent.state='READY'
CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step(value)
JOIN control_plane.runtime_revisions revision ON revision.node_id=node.id AND revision.run_id=node.run_id AND revision.root_run_id=root.id
    AND revision.organization_id=node.organization_id AND revision.project_id=root.project_id AND revision.agent_id=node.agent_id AND revision.turn_id=node.turn_id AND revision.attempt=node.attempt
JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id AND lease.node_id=node.id AND lease.run_id=node.run_id
    AND lease.organization_id=node.organization_id AND lease.generation=revision.generation AND lease.input_digest=revision.input_digest AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
JOIN control_plane.integration_connections connection ON connection.organization_id=root.organization_id AND connection.ref=@connection_ref
    AND connection.enabled AND connection.state='CONNECTED'
WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid
    AND root.state IN ('RUNNING','WAITING_HUMAN') AND physical.ref=@run_ref AND node.ref=@node_ref
    AND node.state='RUNNING' AND node.materialization_state='MATERIALIZED' AND node.type='AGENT_EXECUTION'
    AND node.workflow_business_attempt BETWEEN 1 AND 4 AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    AND step.value->>'Key'=node.workflow_step_key AND step.value->>'AgentRef'=agent.ref
    AND control_plane.runtime_execution_before_deadline(root.organization_id,node.run_id)
    AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_revisions later WHERE later.node_id=node.id AND later.generation>revision.generation)
    AND EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) pin
        WHERE pin->>'ref'=@grant_ref AND pin->>'grantVersion'=(@grant_version::bigint)::text
            AND pin->>'connectionRef'=@connection_ref AND pin->>'operation'=@operation
            AND pin->>'definitionDigest'=@definition_digest AND pin->>'approvalPolicy'=@approval_policy)
FOR UPDATE OF anchor;
