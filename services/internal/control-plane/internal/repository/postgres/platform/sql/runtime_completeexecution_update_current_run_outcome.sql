-- name: runtime_completeexecution_update_current_run_outcome :exec
WITH workflow_evidence AS MATERIALIZED (
  SELECT EXISTS (
    SELECT 1 FROM control_plane.run_nodes node
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    JOIN control_plane.integration_invocations invocation ON invocation.node_id=node.id AND invocation.organization_id=node.organization_id AND invocation.run_id=node.run_id
    WHERE node.run_id=$1::uuid AND node.workflow_business_attempt>0 AND node.owner_rework_gate_id IS NULL
      AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
      AND version.spec->>'ResultEvidenceProfile'='GITHUB_REVIEW_QA_V1'
      AND invocation.operation IN ('github.pull_request.review.create','github.issue.create')
      AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges delivered
        WHERE delivered.organization_id=node.organization_id AND delivered.root_run_id=root.id
          AND delivered.source_node_id=node.id AND delivered.type='CONTINUES')
  ) AS needs_delivery
), owned_read AS MATERIALIZED (
  SELECT EXISTS (
    SELECT 1 FROM control_plane.run_nodes node
    JOIN control_plane.integration_invocations invocation ON invocation.node_id=node.id
      AND invocation.organization_id=node.organization_id AND invocation.run_id=node.run_id AND invocation.risk='READ'
    WHERE node.run_id=$1::uuid AND node.owner_rework_gate_id IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges next_node WHERE next_node.source_node_id=node.id AND next_node.type='CONTINUES')
  ) AS pending
)
UPDATE control_plane.runs target
SET state=CASE WHEN $2='SUCCEEDED' AND (
      EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_run_id=target.id AND launch.state='OPEN')
      OR control_plane.repository_bundle_callback_pending(target.organization_id,target.id)
      OR (SELECT pending FROM owned_read) OR (SELECT needs_delivery FROM workflow_evidence)) THEN 'RUNNING' ELSE $2 END,
    result_summary=$3,safe_error_code=$4,safe_error_message=$5,
    finished_at=CASE WHEN $2='SUCCEEDED' AND (
      EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_run_id=target.id AND launch.state='OPEN')
      OR control_plane.repository_bundle_callback_pending(target.organization_id,target.id)
      OR (SELECT pending FROM owned_read) OR (SELECT needs_delivery FROM workflow_evidence)) THEN NULL ELSE clock_timestamp() END,
    version=version+1,updated_at=clock_timestamp()
WHERE target.id=$1::uuid AND target.id<>target.root_run_id
