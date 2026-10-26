-- name: ssh_compose__predecessor :one
WITH consumer AS MATERIALIZED (
 SELECT node.*,version.spec
 FROM control_plane.run_nodes node
 JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
 LEFT JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
 WHERE node.id=@node_id::uuid AND node.organization_id=@organization_id::uuid AND root.project_id=@project_id::uuid
)
SELECT i.ssh_compose_snapshot,COALESCE(receipt.result_summary,''),COALESCE(receipt.response_digest,''),
       i.ref,i.input_digest,i.effect_key
FROM consumer CROSS JOIN control_plane.integration_invocations i
JOIN control_plane.runs producer ON producer.id=i.run_id
JOIN control_plane.run_nodes origin ON origin.id=i.node_id AND origin.organization_id=i.organization_id
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=i.effect_receipt_id
WHERE i.organization_id=@organization_id::uuid AND i.ref=@predecessor_ref
  AND i.connection_id=@connection_id::uuid AND producer.project_id=@project_id::uuid
  AND i.operation=@operation
  AND ((@operation='ssh.compose.inspect' AND i.state='SUCCEEDED'
    AND receipt.effect_key=i.effect_key AND receipt.input_digest=i.input_digest)
    OR (@operation='ssh.compose.deploy' AND i.state IN ('RUNNING','UNKNOWN_OUTCOME','SUCCEEDED','FAILED','CANCELLED')))
  AND i.ssh_compose_snapshot IS NOT NULL
AND (COALESCE(consumer.spec->>'ExecutionMode','')=''  OR (consumer.spec->>'ExecutionMode'='SERVER_DAG_V1' AND
    producer.root_run_id=consumer.root_run_id AND origin.root_run_id=consumer.root_run_id
    AND origin.agent_id=consumer.agent_id AND origin.workflow_step_key=consumer.workflow_step_key
    AND COALESCE(origin.workflow_step_origin_node_id,origin.id)=COALESCE(consumer.workflow_step_origin_node_id,consumer.id)
    AND origin.workflow_business_attempt BETWEEN 1 AND consumer.workflow_business_attempt
  ))
FOR SHARE OF i NOWAIT;
