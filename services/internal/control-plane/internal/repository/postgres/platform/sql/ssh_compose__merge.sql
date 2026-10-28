-- name: ssh_compose__merge :one
WITH consumer AS MATERIALIZED (
 SELECT node.id,node.root_run_id,node.workflow_step_key,node.workflow_inputs,version.spec
 FROM control_plane.run_nodes node
 JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=node.organization_id
 JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
 LEFT JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
 WHERE node.organization_id=@organization_id::uuid AND node.id=@node_id::uuid
   AND physical.project_id=@project_id::uuid AND root.project_id=physical.project_id
)
SELECT m.resource_scope,receipt.result_summary,receipt.response_digest,c.ref,
       c.definition_version,c.definition_digest,
       cr.ref,cr.revision,cr.secret_ref,cr.secret_uid,cr.secret_resource_version,cr.content_sha256
FROM consumer CROSS JOIN control_plane.integration_invocations m
JOIN control_plane.runs producer ON producer.id=m.run_id
JOIN control_plane.integration_effect_receipts receipt ON receipt.id=m.effect_receipt_id
  AND receipt.invocation_id=m.id AND receipt.organization_id=m.organization_id
JOIN control_plane.integration_connections c ON c.id=m.connection_id
JOIN control_plane.integration_credential_revisions cr ON cr.id=c.credential_revision_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
WHERE m.organization_id=@organization_id::uuid AND m.ref=@merge_ref
  AND producer.project_id=@project_id::uuid
  AND m.state='SUCCEEDED' AND m.operation='github.pull_request.merge'
  AND receipt.effect_key=m.effect_key AND receipt.input_digest=m.input_digest
  AND c.enabled AND c.state='CONNECTED' AND c.definition_key='github'
  AND d.enabled AND d.adapter_readiness='READY'
  AND c.public_configuration->>'owner'=@owner
  AND c.public_configuration->>'repository'=@repository
  AND m.resource_scope->>'owner'=@owner AND m.resource_scope->>'repository'=@repository
AND (COALESCE(consumer.spec->>'ExecutionMode','')=''  OR (consumer.spec->>'ExecutionMode'='SERVER_DAG_V1' AND
    producer.root_run_id=consumer.root_run_id AND EXISTS (
      SELECT 1 FROM jsonb_array_elements(consumer.spec->'Steps') step
      CROSS JOIN LATERAL jsonb_array_elements(step->'ResultRequirements') requirement
      CROSS JOIN LATERAL jsonb_array_elements(consumer.workflow_inputs) input
      JOIN control_plane.run_nodes source ON source.id::text=input->>'sourceNodeId'
        AND source.organization_id=m.organization_id AND source.root_run_id=consumer.root_run_id
        AND source.workflow_step_origin_node_id IS NULL AND source.workflow_business_attempt=1
        AND source.workflow_step_key=input->>'stepKey'
      CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) journal
      CROSS JOIN LATERAL jsonb_array_elements(journal->'results') result
      WHERE step->>'Key'=consumer.workflow_step_key AND requirement->>'Kind'='DEPLOYMENT'
        AND requirement->>'Operation'='ssh.compose.deploy' AND requirement->>'TargetRef'=@target_ref
        AND requirement->>'Repository'=@owner||'/'||@repository
        AND requirement->>'SubjectStepKey'=input->>'stepKey' AND step->'DependsOn' ? (input->>'stepKey')
        AND journal=input->'receipt' AND journal->>'accepted'='true'
        AND result->>'kind'='INTEGRATION_RECEIPT' AND result->>'receiptRef'=receipt.ref
        AND result->>'repository'=@owner||'/'||@repository
        AND result->>'headSha'=receipt.result_summary::jsonb->>'sha'
        AND 1=(SELECT count(*) FROM jsonb_array_elements(journal->'results') subject
          WHERE subject->>'kind' IN('PULL_REQUEST','INTEGRATION_RECEIPT'))
    )
  ))
FOR SHARE OF m,c,cr NOWAIT;
