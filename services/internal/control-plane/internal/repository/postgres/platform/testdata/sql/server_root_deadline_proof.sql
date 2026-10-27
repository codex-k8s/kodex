-- name: server_root_deadline_proof :one
SELECT jsonb_build_object(
 'rootState',root.state,'rootCode',root.safe_error_code,'rootVersion',root.version,
 'startedAt',root.execution_started_at,'deadlineAt',root.execution_deadline_at,
 'rootProcessState',(SELECT state FROM control_plane.run_nodes WHERE organization_id=root.organization_id AND run_id=root.id AND type='ROOT_PROCESS' AND parent_node_id IS NULL),
 'rootAgentNodes',(SELECT count(*) FROM control_plane.run_nodes WHERE organization_id=root.organization_id AND run_id=root.id AND type='AGENT_EXECUTION'),
 'plannedNodes',(SELECT count(*) FROM control_plane.run_nodes WHERE organization_id=root.organization_id AND root_run_id=root.id AND materialization_state='PLANNED'),
 'plannedOpen',(SELECT count(*) FROM control_plane.run_nodes WHERE organization_id=root.organization_id AND root_run_id=root.id AND materialization_state='PLANNED' AND state='PLANNED'),
 'plannedCancelled',(SELECT count(*) FROM control_plane.run_nodes WHERE organization_id=root.organization_id AND root_run_id=root.id AND materialization_state='PLANNED' AND state='CANCELLED' AND turn_id IS NULL),
 'activeRuns',(SELECT count(*) FROM control_plane.runs WHERE organization_id=root.organization_id AND root_run_id=root.id AND state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')),
 'activeModels',(SELECT count(*) FROM control_plane.runtime_leases lease JOIN control_plane.runs run ON run.id=lease.run_id AND run.organization_id=lease.organization_id WHERE run.organization_id=root.organization_id AND run.root_run_id=root.id AND lease.state='CLAIMED'),
 'activeInvocations',(SELECT count(*) FROM control_plane.integration_invocations invocation JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id WHERE run.organization_id=root.organization_id AND run.root_run_id=root.id AND invocation.state IN ('WAITING_APPROVAL','READY','RUNNING')),
 'openGates',(SELECT count(*) FROM control_plane.owner_gates WHERE organization_id=root.organization_id AND root_run_id=root.id AND state='OPEN'),
 'events',(SELECT count(*) FROM control_plane.run_events WHERE organization_id=root.organization_id AND root_run_id=root.id),
 'custody',(SELECT COALESCE(jsonb_agg(jsonb_build_object('id',node.id,'run',node.run_id,'turn',node.turn_id,'materialization',node.materialization_state,'business',node.workflow_business_attempt,'capture',node.workflow_captured_artifacts,'journal',node.workflow_attempts,'inputs',node.workflow_inputs,'intents',node.workflow_effect_intents,'rejoins',node.workflow_effect_rejoins) ORDER BY node.id),'[]'::jsonb) FROM control_plane.run_nodes node WHERE node.organization_id=root.organization_id AND node.root_run_id=root.id),
 'episodeHistory',root.workflow_exception_history)
FROM control_plane.runs root JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
WHERE root.ref=@run_ref AND project.ref=@project_ref AND root.id=root.root_run_id;
