-- name: workflow_evidence__issue_pin :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.run_nodes final
    JOIN control_plane.run_nodes anchor ON anchor.id=COALESCE(final.workflow_step_origin_node_id,final.id)
        AND anchor.organization_id=final.organization_id AND anchor.root_run_id=final.root_run_id
        AND anchor.agent_id=final.agent_id AND anchor.workflow_step_key=final.workflow_step_key
    CROSS JOIN LATERAL jsonb_array_elements(anchor.workflow_effect_intents) intent(value)
    JOIN control_plane.integration_invocations invocation ON invocation.ref=intent.value->>'invocationRef'
        AND invocation.organization_id=final.organization_id
    JOIN control_plane.runs producing_run ON producing_run.id=invocation.run_id AND producing_run.organization_id=final.organization_id
        AND producing_run.root_run_id=final.root_run_id
        AND invocation.operation='github.issue.create' AND invocation.state='SUCCEEDED'
    JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id
        AND receipt.organization_id=final.organization_id AND receipt.invocation_id=invocation.id
        AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
    WHERE final.organization_id=@organization_id::uuid AND final.id=@node_id::uuid AND final.root_run_id=@root_run_id::uuid
        AND final.workflow_captured_artifacts->>'runtimeRevisionId'=@runtime_revision_id::text
        AND final.workflow_captured_artifacts->>'leaseId'=@lease_id::text
        AND intent.value->>'operation'='github.issue.create' AND intent.value->>'resultKey'=@result_key
        AND intent.value->'originBusinessAttempt'=to_jsonb(final.workflow_business_attempt)
        AND intent.value->'qaReport'=@qa_report::jsonb AND receipt.ref=@receipt_ref
);
