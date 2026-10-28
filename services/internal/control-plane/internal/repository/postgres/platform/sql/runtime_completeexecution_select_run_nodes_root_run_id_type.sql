-- name: runtime_completeexecution_select_run_nodes_root_run_id_type :one
SELECT (
    SELECT count(*)
    FROM control_plane.run_nodes
    WHERE root_run_id=$1::uuid
      AND type='AGENT_EXECUTION'
      AND state IN('QUEUED','RUNNING','WAITING')
) + CASE WHEN control_plane.legacy_rework_delivery_pending((SELECT organization_id FROM control_plane.runs WHERE id=$1::uuid),$1::uuid) THEN 1 ELSE 0 END + (
    SELECT count(*)
    FROM control_plane.owner_gates
    WHERE root_run_id=$1::uuid
      AND state='OPEN'
) + (
    SELECT count(*) FROM control_plane.required_workflow_launches
    WHERE origin_root_run_id=$1::uuid AND state='OPEN'
) + (
    SELECT count(*) FROM control_plane.repository_bundle_publications
    WHERE root_run_id=$1::uuid
      AND (state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME') OR continuation_state='BLOCKED')
), (
    SELECT count(*)
    FROM control_plane.run_nodes
    WHERE root_run_id=$1::uuid
      AND type='AGENT_EXECUTION'
      AND state='PLANNED'
      AND materialization_state='PLANNED'
)
