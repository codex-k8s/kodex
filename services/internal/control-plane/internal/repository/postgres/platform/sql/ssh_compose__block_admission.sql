-- name: ssh_compose__block_admission :one
WITH pinned AS MATERIALIZED (
 SELECT invocation.id,node.ref AS node_ref,run.project_id,root.id AS root_id
 FROM control_plane.integration_invocations invocation
 JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
   AND connection.organization_id=invocation.organization_id AND connection.definition_key='ssh-compose'
 JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
 JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
   AND node.run_id=run.id AND node.root_run_id=run.root_run_id
 JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
 WHERE invocation.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid
   AND invocation.state=@expected_state AND invocation.state IN('READY','UNKNOWN_OUTCOME')
   AND invocation.generation=@expected_generation::bigint
   AND invocation.input_digest=@expected_input_digest
   AND invocation.ssh_compose_snapshot IS NOT DISTINCT FROM @expected_snapshot::jsonb
   AND NOT invocation.ssh_compose_admission_blocked AND invocation.effect_receipt_id IS NULL
   AND invocation.lease_ref IS NULL AND invocation.effect_fence_digest IS NULL
   AND invocation.workload_instance IS NULL AND invocation.lease_expires_at IS NULL
   AND root.project_id=run.project_id
   AND root.state IN('RUNNING','WAITING_HUMAN')
 FOR UPDATE OF root NOWAIT
), blocked AS (
 UPDATE control_plane.integration_invocations invocation
 SET ssh_compose_admission_blocked=true,ssh_compose_admission_block_reason='CURRENT_ADMISSION_DENIED',
     version=version+1,updated_at=clock_timestamp()
 FROM pinned WHERE invocation.id=pinned.id
 RETURNING invocation.id,invocation.ref
)
SELECT blocked.ref,pinned.node_ref,pinned.project_id::text,pinned.root_id::text
FROM blocked JOIN pinned ON pinned.id=blocked.id;
