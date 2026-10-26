-- name: workflow_dispatch_proof :one
WITH root AS (
    SELECT run.id,run.organization_id,run.project_id,run.ref,run.state
    FROM control_plane.runs run
    JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=run.organization_id
    WHERE run.ref=@run_ref AND project.ref=@project_ref AND run.root_run_id=run.id
), nodes AS (
    SELECT node.* FROM control_plane.run_nodes node
    JOIN root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
)
SELECT jsonb_build_object(
    'rootId',root.id::text,'rootState',root.state,
    'managerExecutions',(SELECT count(*) FROM nodes JOIN control_plane.agents agent
        ON agent.id=nodes.agent_id AND agent.organization_id=nodes.organization_id
        WHERE agent.ref=@manager_ref AND nodes.type='AGENT_EXECUTION'),
    'activeAgentExecutions',(SELECT count(*) FROM nodes
        WHERE nodes.type='AGENT_EXECUTION' AND nodes.state='RUNNING'),
    'openLeases',(SELECT count(*) FROM control_plane.runtime_leases lease
        JOIN nodes ON nodes.id=lease.node_id AND nodes.organization_id=lease.organization_id
        WHERE lease.state='CLAIMED'),
    'anchors',COALESCE((SELECT jsonb_agg(jsonb_build_object(
        'id',node.id::text,'ref',node.ref,'stepKey',node.workflow_step_key,
        'agentRef',agent.ref,'state',node.state,'runRef',run.ref,'sessionId',run.session_id::text,
        'turnId',node.turn_id::text,'turnRef',turn.ref,'runtimeAttempt',node.attempt,
        'businessAttempt',node.workflow_business_attempt,'materializationState',node.materialization_state,
        'inputs',node.workflow_inputs,'history',node.workflow_attempts,'capture',node.workflow_captured_artifacts,
        'executions',COALESCE((SELECT jsonb_agg(jsonb_build_object(
            'revisionId',revision.id::text,'revisionRef',revision.ref,'turnId',revision.turn_id::text,
            'runId',revision.run_id::text,'rootRunId',revision.root_run_id::text,
            'nodeId',revision.node_id::text,'runtimeAttempt',revision.attempt,
            'leaseId',lease.id::text,'leaseRef',lease.ref,'leaseState',lease.state,'generation',lease.generation
        ) ORDER BY revision.generation)
        FROM control_plane.runtime_revisions revision
        JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
            AND lease.organization_id=revision.organization_id AND lease.node_id=revision.node_id
            AND lease.run_id=revision.run_id AND lease.generation=revision.generation
        WHERE revision.node_id=node.id AND revision.organization_id=node.organization_id),'[]'::jsonb)
    ) ORDER BY node.workflow_step_key)
    FROM nodes node
    JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=node.organization_id
    JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
    LEFT JOIN control_plane.session_turns turn ON turn.id=node.turn_id AND turn.organization_id=node.organization_id
    WHERE node.workflow_step_key IN ('producer','review-a','review-b')
        AND node.workflow_step_origin_node_id IS NULL),'[]'::jsonb)
)
FROM root;
