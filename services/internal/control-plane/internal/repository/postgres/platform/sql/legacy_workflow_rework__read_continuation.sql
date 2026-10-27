-- name: legacy_workflow_rework__read_continuation :one
SELECT node.id::text,node.run_id::text,node.root_run_id::text,node.agent_id::text,node.attempt,
    node.display_name,node.role,physical.session_id::text,agent.ref,root.workflow_version_id::text,
    jsonb_build_object('schema','kodex.legacy-owner-rework-read.v1','completedReads',(
        SELECT jsonb_agg(jsonb_build_object('invocationRef',fact.ref,'state',fact.state,
            'resultSummary',fact.result_summary,'safeErrorCode',fact.safe_error_code,
            'receiptRef',COALESCE(receipt.ref,''),'inputDigest',fact.input_digest,
            'grantRef',grant_record.ref,'grantVersion',fact.grant_version,'definitionDigest',fact.definition_digest)
            ORDER BY fact.ref)
        FROM control_plane.integration_invocations fact
        JOIN control_plane.integration_grants grant_record ON grant_record.id=fact.grant_id AND grant_record.organization_id=root.organization_id
        LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=fact.effect_receipt_id
            AND receipt.organization_id=root.organization_id AND receipt.invocation_id=fact.id
            AND receipt.effect_key=fact.effect_key AND receipt.input_digest=fact.input_digest
        WHERE fact.organization_id=root.organization_id AND fact.node_id=node.id AND fact.run_id=physical.id AND fact.risk='READ'
    ))
FROM control_plane.integration_invocations invocation
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=node.organization_id
    AND physical.id=invocation.run_id AND physical.state='RUNNING'
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    AND root.id=physical.root_run_id AND root.project_id=physical.project_id AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=root.organization_id
    AND agent.project_id=root.project_id AND agent.enabled AND agent.state='READY'
JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id AND project.lifecycle='ACTIVE'
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id
    AND workflow.project_id=root.project_id AND workflow.ref=root.target_ref
JOIN control_plane.catalog_access_targets target ON target.organization_id=root.organization_id AND target.kind='WORKFLOW' AND target.id=workflow.id
WHERE invocation.id=@invocation_id::uuid AND invocation.organization_id=@organization_id::uuid
    AND invocation.risk='READ' AND invocation.approval_policy='NONE'
    AND node.owner_rework_gate_id IS NOT NULL AND node.state='SUCCEEDED' AND node.finished_at IS NOT NULL
    AND control_plane.legacy_rework_execution_assigned(node.organization_id,node.id)
    AND control_plane.catalog_resource_visible(root.organization_id,actor.id,'workflow.launch','WORKFLOW',target.id,
        target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
    AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges next_node WHERE next_node.organization_id=root.organization_id
        AND next_node.root_run_id=root.id AND next_node.source_node_id=node.id AND next_node.type='CONTINUES')
    AND NOT EXISTS (SELECT 1 FROM control_plane.integration_invocations fact
        LEFT JOIN control_plane.integration_grants grant_record ON grant_record.id=fact.grant_id AND grant_record.organization_id=root.organization_id
        LEFT JOIN control_plane.integration_connections connection ON connection.id=fact.connection_id AND connection.organization_id=root.organization_id
        WHERE fact.organization_id=root.organization_id AND fact.node_id=node.id AND fact.run_id=physical.id
            AND (fact.risk<>'READ' OR fact.approval_policy<>'NONE'
                OR fact.state NOT IN ('SUCCEEDED','FAILED','REJECTED','CANCELLED')
                OR EXISTS (SELECT 1 FROM control_plane.owner_gates gate WHERE gate.integration_invocation_id=fact.id)
                OR NOT COALESCE(grant_record.enabled AND grant_record.version=fact.grant_version
                    AND grant_record.target_kind='AGENT' AND grant_record.target_ref=agent.ref
                    AND grant_record.connection_id=connection.id AND grant_record.capability_key=fact.capability_key
                    AND grant_record.risk='READ' AND grant_record.approval_policy='NONE'
                    AND grant_record.definition_version=fact.definition_version AND grant_record.definition_digest=fact.definition_digest
                    AND connection.enabled AND connection.state='CONNECTED' AND connection.lifecycle_state='ACTIVE' AND connection.definition_version=fact.definition_version
                    AND connection.definition_digest=fact.definition_digest
                    AND EXISTS (SELECT 1 FROM control_plane.integration_definitions definition WHERE definition.stable_key=connection.definition_key
                        AND definition.enabled AND definition.adapter_readiness='READY')
                    AND control_plane.catalog_resource_visible(root.organization_id,actor.id,'integration.manage','INTEGRATION',connection.id,
                        NULL::uuid,connection.created_by,'{}'::jsonb,statement_timestamp(),false),false)
                OR (fact.state='SUCCEEDED' AND NOT EXISTS (SELECT 1 FROM control_plane.integration_effect_receipts receipt
                    WHERE receipt.id=fact.effect_receipt_id AND receipt.organization_id=root.organization_id AND receipt.invocation_id=fact.id
                        AND receipt.effect_key=fact.effect_key AND receipt.input_digest=fact.input_digest AND receipt.result_summary=fact.result_summary))))
FOR UPDATE OF node,root;
