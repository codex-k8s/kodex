WITH grant_seed AS (
    INSERT INTO control_plane.integration_grants
      (ref,organization_id,connection_id,capability_key,target_kind,target_ref,enabled,created_by,
       risk,resource_kind,resource_scope,resource_scope_digest,definition_version,definition_digest,approval_policy)
    SELECT 'email_receipt_grant',connection.organization_id,connection.id,@capability_key,'AGENT',@agent_ref,false,connection.created_by,
      @risk,@resource_kind,@resource_scope::jsonb,@resource_scope_digest,@definition_version,@definition_digest,@approval_policy
    FROM control_plane.integration_connections connection
    JOIN control_plane.projects project ON project.organization_id=connection.organization_id AND project.ref=@project_ref
    WHERE connection.ref=@connection_ref AND connection.definition_key='email'
      AND (connection.definition_version,connection.definition_digest)=(@definition_version,@definition_digest)
    RETURNING *
), grant_row AS (
    SELECT grant_row.id,grant_row.organization_id,grant_row.connection_id,grant_row.capability_key,
      grant_row.version,grant_row.approval_policy,grant_row.risk,grant_row.resource_kind,
      grant_row.definition_version,grant_row.definition_digest,grant_row.resource_scope,grant_row.resource_scope_digest,
      capability->>'operation' AS operation
    FROM grant_seed grant_row
    JOIN control_plane.integration_connections connection ON connection.id=grant_row.connection_id
      AND connection.organization_id=grant_row.organization_id
    CROSS JOIN LATERAL jsonb_array_elements(control_plane.integration_pinned_capabilities(connection.id)) capability
    JOIN control_plane.projects project ON project.organization_id=connection.organization_id AND project.ref=@project_ref
    WHERE connection.ref=@connection_ref AND connection.definition_key='email'
      AND grant_row.target_kind='AGENT' AND grant_row.target_ref=@agent_ref
      AND grant_row.capability_key=@capability_key AND NOT grant_row.enabled
      AND capability->>'key'=grant_row.capability_key
      AND capability->>'operation'=@operation
      AND capability->>'risk'=grant_row.risk
      AND COALESCE(capability->>'resourceKind',capability #>> '{resourceScope,kind}')=grant_row.resource_kind
      AND capability->>'approvalPolicy'=grant_row.approval_policy
      AND (grant_row.definition_version,grant_row.definition_digest)=(connection.definition_version,connection.definition_digest)
), invocation AS (
    INSERT INTO control_plane.integration_invocations
        (ref,organization_id,run_id,node_id,connection_id,grant_id,capability_key,operation,idempotency_key,
         intent_digest,input_digest,bounded_input,state,effect_key,risk,approval_policy,resource_kind,grant_version,
         definition_version,definition_digest,resource_scope,resource_scope_digest,mailbox_gate_required)
    SELECT 'email_receipt_invocation',r.organization_id,r.id,n.id,g.connection_id,g.id,g.capability_key,g.operation,'email-receipt-fixture',
           repeat('a',64),repeat('a',64),'{}','UNKNOWN_OUTCOME','eff_opaque:email',g.risk,g.approval_policy,g.resource_kind,g.version,
           g.definition_version,g.definition_digest,g.resource_scope,g.resource_scope_digest,true
    FROM control_plane.runs r JOIN control_plane.run_nodes n ON n.run_id=r.id
    JOIN control_plane.agents agent ON agent.id=n.agent_id AND agent.organization_id=r.organization_id AND agent.ref=@agent_ref
    JOIN grant_row g ON g.organization_id=r.organization_id
    WHERE r.ref=@run_ref ORDER BY n.ref LIMIT 1 RETURNING *
)
INSERT INTO control_plane.email_effect_receipts
    (ref,organization_id,invocation_id,external_receipt_ref,external_receipt_digest,semantic_input_digest,effect_key,mailbox_ref,configuration_revision)
SELECT 'emrc_email_fixture',organization_id,id,repeat('b',32),repeat('c',64),repeat('a',64),effect_key,'fixture-mailbox',1
FROM invocation;
