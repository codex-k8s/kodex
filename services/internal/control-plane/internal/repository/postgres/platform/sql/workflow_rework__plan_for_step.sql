-- name: workflow_rework__plan_for_step :one
SELECT episode.value
FROM control_plane.runs root
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
CROSS JOIN LATERAL jsonb_array_elements(root.workflow_exception_history) WITH ORDINALITY episode(value,position)
JOIN control_plane.owner_gates gate ON gate.id::text=episode.value->>'gateId' AND gate.ref=episode.value->>'gateRef'
    AND gate.organization_id=root.organization_id AND gate.root_run_id=root.id AND gate.project_id=root.project_id
    AND gate.integration_invocation_id IS NULL AND gate.state='APPROVED' AND gate.decision='APPROVE'
WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND root.id=root.root_run_id
    AND root.state IN ('RUNNING','WAITING_HUMAN') AND root.workflow_exception IS NULL
    AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1' AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    AND episode.value->>'kind'='UPSTREAM_REWORK' AND episode.value->>'state'='RECOVERED'
    AND episode.value->'reworkPlan'->>'schema'='kodex.workflow-upstream-rework.v1'
    AND COALESCE(episode.value->'reworkPlan'->>'blockReason','')=''
    AND EXISTS(SELECT 1 FROM jsonb_array_elements((episode.value->'reworkPlan'->'targets')||(episode.value->'reworkPlan'->'rechecks')) target(value)
        WHERE target.value->>'stepKey'=@step_key AND target.value->>'originNodeId'=@origin_node_id
            AND target.value->'previousAttempt'=to_jsonb(@current_attempt::integer)
            AND target.value->'nextAttempt'=to_jsonb(@current_attempt::integer+1))
ORDER BY episode.position DESC LIMIT 1;
