WITH connection AS (
    INSERT INTO control_plane.integration_connections
        (ref,organization_id,definition_key,name,state,masked_credentials_state,created_by,definition_version,definition_digest)
    SELECT 'email_receipt_connection',project.organization_id,'email','Email receipt fixture','CONNECTED','CONFIGURED',project.created_by,definition.definition_version,definition.digest
    FROM control_plane.projects project JOIN control_plane.integration_definitions definition ON definition.stable_key='email' WHERE project.ref=$1
    RETURNING *
), grant_row AS (
    INSERT INTO control_plane.integration_grants
        (ref,organization_id,connection_id,capability_key,target_kind,target_ref,created_by,resource_kind,definition_version,definition_digest,approval_policy)
    SELECT 'email_receipt_grant',organization_id,id,'email.message.send','AGENT',$3,created_by,'EMAIL_SENDER',definition_version,definition_digest,'NONE'
    FROM connection RETURNING *
), invocation AS (
    INSERT INTO control_plane.integration_invocations
        (ref,organization_id,run_id,node_id,connection_id,grant_id,capability_key,operation,idempotency_key,
         intent_digest,input_digest,bounded_input,state,effect_key,risk,approval_policy,resource_kind,grant_version)
    SELECT 'email_receipt_invocation',r.organization_id,r.id,n.id,g.connection_id,g.id,'email.message.send','email.message.send','email-receipt-fixture',
           repeat('a',64),repeat('a',64),'{}','UNKNOWN_OUTCOME','eff_opaque:email','SENSITIVE','NONE','EMAIL_SENDER',g.version
    FROM control_plane.runs r JOIN control_plane.run_nodes n ON n.run_id=r.id CROSS JOIN grant_row g
    WHERE r.ref=$2 ORDER BY n.ref LIMIT 1 RETURNING *
)
INSERT INTO control_plane.email_effect_receipts
    (ref,organization_id,invocation_id,external_receipt_ref,external_receipt_digest,semantic_input_digest,effect_key,mailbox_ref,configuration_revision)
SELECT 'emrc_email_fixture',organization_id,id,repeat('b',32),repeat('c',64),repeat('a',64),effect_key,'fixture-mailbox',1
FROM invocation;
