-- name: workflow_rework__subjects :one
SELECT COALESCE(receipt.value->'reworkSubjects','[]'::jsonb)
FROM control_plane.run_nodes anchor
CROSS JOIN LATERAL jsonb_array_elements(anchor.workflow_attempts) receipt(value)
JOIN control_plane.run_nodes physical ON physical.id::text=receipt.value->'provenance'->>'nodeId'
    AND physical.organization_id=anchor.organization_id AND physical.root_run_id=anchor.root_run_id
    AND COALESCE(physical.workflow_step_origin_node_id,physical.id)=anchor.id
    AND physical.workflow_business_attempt=@business_attempt::integer AND physical.state IN ('SUCCEEDED','FAILED')
WHERE anchor.organization_id=@organization_id::uuid AND anchor.root_run_id=@root_run_id::uuid
    AND anchor.id=@origin_node_id::uuid AND anchor.workflow_step_origin_node_id IS NULL
    AND physical.id=@final_node_id::uuid AND receipt.value->'attempt'=to_jsonb(@business_attempt::integer)
    AND receipt.value->>'accepted'='false' AND receipt.value->>'reason'='CHANGES_REQUIRED'
    AND jsonb_array_length(anchor.workflow_attempts)=@business_attempt::integer;
