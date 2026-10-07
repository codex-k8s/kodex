-- name: workflow_launch_origin_diagnostics :one
SELECT jsonb_build_object('lease',lease.state,'node',node.state,'turn',turn.state,'run',run.state,'root',root.state,
 'enabled',agent.enabled,'agentState',agent.state,'ordinary',agent.system_key IS NULL,
 'revisionCap','platform.run.launch'=ANY(revision.capabilities),'currentCap','platform.run.launch'=ANY(agent.capabilities),
 'assistantScope',revision.safe_snapshot->>'assistantScope',
 'attemptMatch',revision.attempt=node.attempt,'rootMatch',revision.root_run_id=root.id,
 'runMatch',revision.run_id=run.id,'turnMatch',revision.turn_id=turn.id,'workflowStepKey',node.workflow_step_key)
FROM control_plane.runtime_leases lease JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
JOIN control_plane.run_nodes node ON node.id=lease.node_id JOIN control_plane.runs run ON run.id=node.run_id
JOIN control_plane.runs root ON root.id=run.root_run_id JOIN control_plane.agents agent ON agent.id=node.agent_id
JOIN control_plane.session_turns turn ON turn.id=node.turn_id WHERE lease.ref=@lease_ref;
