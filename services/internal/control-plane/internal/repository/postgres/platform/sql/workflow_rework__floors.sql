-- name: workflow_rework__floors :one
WITH required AS (
    SELECT target.value->>'stepKey' AS key,(target.value->>'nextAttempt')::integer AS attempt
    FROM control_plane.runs root
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    CROSS JOIN LATERAL jsonb_array_elements(root.workflow_exception_history) episode(value)
    JOIN control_plane.owner_gates gate ON gate.id::text=episode.value->>'gateId' AND gate.ref=episode.value->>'gateRef'
        AND gate.organization_id=root.organization_id AND gate.root_run_id=root.id AND gate.project_id=root.project_id
        AND gate.integration_invocation_id IS NULL AND gate.state='APPROVED' AND gate.decision='APPROVE'
    CROSS JOIN LATERAL jsonb_array_elements((episode.value->'reworkPlan'->'targets')||(episode.value->'reworkPlan'->'rechecks')) target(value)
    WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND root.id=root.root_run_id
        AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
        AND episode.value->>'kind'='UPSTREAM_REWORK' AND episode.value->>'state' IN ('WAITING_OWNER','RECOVERED')
        AND episode.value->'reworkPlan'->>'schema'='kodex.workflow-upstream-rework.v1'
        AND COALESCE(episode.value->'reworkPlan'->>'blockReason','')=''
), highest AS (SELECT key,max(attempt) AS attempt FROM required GROUP BY key)
SELECT COALESCE(jsonb_object_agg(key,attempt),'{}'::jsonb) FROM highest;
