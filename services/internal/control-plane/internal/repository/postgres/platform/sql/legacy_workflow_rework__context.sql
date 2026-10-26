-- name: legacy_workflow_rework__context :one
SELECT jsonb_build_object(
    'rootId',root.id::text,'rootRef',root.ref,'rootState',root.state,'targetType',root.target_type,
    'projectId',COALESCE(root.project_id::text,''),'projectRef',COALESCE(project.ref,''),
    'actorId',root.initiated_by::text,'actorRef',actor.ref,'actorActive',actor.active,
    'organizationRef',organization.ref,'oldRunId',physical.id::text,'oldRunRef',physical.ref,
    'oldRunState',physical.state,'oldNodeId',node.id::text,'oldNodeRef',node.ref,
    'oldNodeState',node.state,'oldRunAttempt',physical.attempt,'oldNodeAttempt',node.attempt,
    'agentRef',agent.ref,'agentId',agent.id::text,'agentReady',agent.enabled AND agent.state='READY',
    'displayName',node.display_name,'role',node.role,'stepKey',COALESCE(logical_origin.workflow_step_key,node.workflow_step_key),
    'task',physical.task,'input',physical.input,'gateId',gate.id::text,'gateRef',gate.ref,
    'gateVersion',gate.version,'comment',gate.decision_comment,
    'workflowVersionId',COALESCE(root.workflow_version_id::text,''),'workflowRef',root.target_ref,
    'executionMode',COALESCE(version.spec->>'ExecutionMode',''),
    'publishedStep',EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(version.spec->'Steps','[]'::jsonb)) step(value)
        WHERE step.value->>'Key'=logical_origin.workflow_step_key AND step.value->>'AgentRef'=agent.ref
            AND step.value->>'HumanGateAfter'='true'),
    'hasWrite',EXISTS (SELECT 1 FROM control_plane.integration_invocations invocation
        JOIN control_plane.run_nodes producer ON producer.id=invocation.node_id AND producer.organization_id=root.organization_id
            AND producer.root_run_id=root.id
        WHERE invocation.organization_id=root.organization_id AND invocation.risk<>'READ'),
    'requiredCapabilities',COALESCE((SELECT step.value->'RequiredCapabilityKeys'
        FROM jsonb_array_elements(COALESCE(version.spec->'Steps','[]'::jsonb)) step(value)
        WHERE step.value->>'Key'=logical_origin.workflow_step_key AND step.value->>'AgentRef'=agent.ref),'[]'::jsonb),
    'episodeCount',(SELECT count(*) FROM control_plane.run_nodes attempt WHERE attempt.organization_id=root.organization_id
        AND attempt.root_run_id=root.id AND attempt.owner_rework_gate_id IS NOT NULL AND attempt.owner_rework_origin_node_id IS NULL),
    'reworkGateId',COALESCE(node.owner_rework_gate_id::text,''),
    'callbackSourceId',COALESCE(logical_origin.id,node.id)::text)
FROM control_plane.runs root
JOIN control_plane.run_nodes node ON node.id=@predecessor_node_id::uuid AND node.organization_id=root.organization_id
    AND node.root_run_id=root.id
JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=root.organization_id
    AND physical.root_run_id=root.id AND physical.project_id IS NOT DISTINCT FROM root.project_id
JOIN control_plane.owner_gates gate ON gate.id=@gate_id::uuid AND gate.organization_id=root.organization_id
    AND gate.root_run_id=root.id AND gate.project_id IS NOT DISTINCT FROM root.project_id
JOIN control_plane.organizations organization ON organization.id=root.organization_id
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=root.organization_id
    AND agent.project_id IS NOT DISTINCT FROM root.project_id
LEFT JOIN control_plane.projects project ON project.id=root.project_id
LEFT JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
LEFT JOIN LATERAL control_plane.legacy_workflow_step_origin(root.organization_id,node.id) origin_id ON true
LEFT JOIN control_plane.run_nodes logical_origin ON logical_origin.id=origin_id AND logical_origin.organization_id=root.organization_id
WHERE root.id=@root_run_id::uuid AND root.organization_id=@organization_id::uuid
FOR UPDATE OF root,node,physical;
