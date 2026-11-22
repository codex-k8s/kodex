-- name: ssh_execution__read :one
SELECT intent.id::text,intent.ref,intent.snapshot_digest,intent.snapshot_body,invocation.state,
 intent.workload_instance,intent.lease_ref,intent.fence_digest,intent.generation,intent.input_digest,
 observation.intent_id IS NOT NULL,COALESCE(observation.outcome,''),COALESCE(observation.result_body,''),
 COALESCE(observation.result_digest,''),COALESCE(observation.applied,false),COALESCE(observation.terminal_state,''),
 invocation.id::text,node.ref,run.root_run_id::text,COALESCE(run.project_id::text,''),intent.created_at,intent.expires_at
FROM control_plane.ssh_execution_intents intent
JOIN control_plane.integration_invocations invocation ON invocation.id=intent.invocation_id AND invocation.organization_id=intent.organization_id
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
LEFT JOIN control_plane.ssh_execution_observations observation ON observation.intent_id=intent.id AND observation.organization_id=intent.organization_id
WHERE intent.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
FOR UPDATE OF invocation;
