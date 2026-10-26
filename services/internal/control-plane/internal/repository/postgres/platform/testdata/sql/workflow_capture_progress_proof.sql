-- name: workflow_capture_progress_proof :one
WITH root AS (
    SELECT run.* FROM control_plane.runs run
    JOIN control_plane.projects project ON project.id=run.project_id AND project.organization_id=run.organization_id
    WHERE run.ref=@run_ref AND project.ref=@project_ref AND run.id=run.root_run_id
), nodes AS (
    SELECT node.* FROM control_plane.run_nodes node
    JOIN root ON node.root_run_id=root.id AND node.organization_id=root.organization_id
    WHERE node.type='AGENT_EXECUTION'
)
SELECT jsonb_build_object(
    'rootId',root.id::text,'state',root.state,
    'runCount',(SELECT count(*) FROM control_plane.runs run WHERE run.root_run_id=root.id AND run.organization_id=root.organization_id),
    'nodeCount',(SELECT count(*) FROM nodes),
    'continues',(SELECT count(*) FROM control_plane.run_edges edge WHERE edge.root_run_id=root.id AND edge.organization_id=root.organization_id AND edge.type='CONTINUES'),
    'eventCount',(SELECT count(*) FROM control_plane.run_events event WHERE event.root_run_id=root.id AND event.organization_id=root.organization_id),
    'journals',(SELECT COALESCE(sum(jsonb_array_length(node.workflow_attempts)),0) FROM nodes node),
    'managers',(SELECT count(*) FROM nodes node JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=node.organization_id WHERE agent.ref=@manager_ref),
    'claimedLeases',(SELECT count(*) FROM control_plane.runtime_leases lease JOIN nodes node ON node.id=lease.node_id AND node.organization_id=lease.organization_id WHERE lease.state='CLAIMED'),
    'nodes',COALESCE((SELECT jsonb_agg(jsonb_build_object(
        'id',node.id::text,'ref',node.ref,'originNodeId',COALESCE(node.workflow_step_origin_node_id,node.id)::text,
        'stepKey',node.workflow_step_key,'agentRef',agent.ref,'state',node.state,
        'runRef',run.ref,'sessionId',run.session_id::text,'turnId',COALESCE(node.turn_id::text,''),'turnRef',COALESCE(turn.ref,''),
        'runtimeAttempt',node.attempt,'businessAttempt',node.workflow_business_attempt,
        'history',node.workflow_attempts,'inputs',node.workflow_inputs,'capture',node.workflow_captured_artifacts,
        'executions',COALESCE((SELECT jsonb_agg(jsonb_build_object(
            'revisionId',revision.id::text,'revisionRef',revision.ref,'turnId',revision.turn_id::text,
            'runId',revision.run_id::text,'rootRunId',revision.root_run_id::text,'nodeId',revision.node_id::text,
            'runtimeAttempt',revision.attempt,'leaseId',lease.id::text,'leaseRef',lease.ref,
            'leaseState',lease.state,'generation',lease.generation
        ) ORDER BY revision.generation)
        FROM control_plane.runtime_revisions revision
        JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id AND lease.organization_id=revision.organization_id
            AND lease.node_id=revision.node_id AND lease.run_id=revision.run_id AND lease.generation=revision.generation
        WHERE revision.node_id=node.id AND revision.organization_id=node.organization_id),'[]'::jsonb)
    ) ORDER BY node.ref) FROM nodes node
    JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=node.organization_id
    JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
    LEFT JOIN control_plane.session_turns turn ON turn.id=node.turn_id AND turn.organization_id=node.organization_id),'[]'::jsonb)
)
FROM root;
