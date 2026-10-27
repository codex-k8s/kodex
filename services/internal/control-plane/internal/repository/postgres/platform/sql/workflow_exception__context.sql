-- name: workflow_exception__context :one
WITH pending AS MATERIALIZED (
 SELECT root.organization_id,root.id,root.project_id,root.workflow_exception AS episode
 FROM control_plane.runs root
 JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
 WHERE root.organization_id=@organization_id::uuid AND root.id=@root_run_id::uuid AND root.id=root.root_run_id
   AND root.target_type='WORKFLOW' AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN')
   AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
   AND root.workflow_exception=root.workflow_exception_history->-1
   AND root.workflow_exception->>'state'='PENDING_MANAGER'
   AND root.workflow_exception->'episode'=to_jsonb(@episode::bigint)
   AND root.workflow_exception->'version'=to_jsonb(@episode_version::bigint)
   AND root.workflow_exception->>'finalNodeId'=@node_id::uuid::text
), decisions AS MATERIALIZED (
 SELECT gate.ref,gate.version,gate.decision,gate.decision_comment,pending.id AS root_id
 FROM pending
 JOIN control_plane.run_edges edge ON edge.organization_id=pending.organization_id AND edge.root_run_id=pending.id
   AND edge.type='WAITING_FOR' AND edge.source_node_id=@node_id::uuid
 JOIN control_plane.run_nodes gate_node ON gate_node.id=edge.target_node_id AND gate_node.organization_id=pending.organization_id
   AND gate_node.root_run_id=pending.id AND gate_node.parent_node_id=@node_id::uuid AND gate_node.type='HUMAN_GATE'
 JOIN control_plane.owner_gates gate ON gate.node_id=gate_node.id AND gate.organization_id=pending.organization_id
   AND gate.root_run_id=pending.id AND gate.project_id=pending.project_id AND gate.integration_invocation_id IS NULL
 WHERE pending.episode->>'reason'='OWNER_CHANGES_REQUESTED'
   AND gate.state='CHANGES_REQUESTED' AND gate.decision='REQUEST_CHANGES' AND btrim(gate.decision_comment)<>''
)
SELECT jsonb_build_object('nodeRef',node.ref,'runRef',physical.ref,'stepKey',node.workflow_step_key,
    'runtimeState',node.state,'safeErrorCode',node.safe_error_code,
    'modelResultData',left(node.progress_summary,1500),
    'capturedArtifactMetadata',COALESCE(node.workflow_captured_artifacts->'artifacts','[]'::jsonb)),
 (SELECT ref FROM decisions WHERE root_id=pending.id),
 (SELECT version FROM decisions WHERE root_id=pending.id),
 (SELECT decision FROM decisions WHERE root_id=pending.id),
 (SELECT decision_comment FROM decisions WHERE root_id=pending.id)
FROM pending
JOIN control_plane.run_nodes node ON node.id=@node_id::uuid AND node.organization_id=pending.organization_id
  AND node.root_run_id=pending.id
JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=pending.organization_id
  AND physical.root_run_id=pending.id AND physical.project_id=pending.project_id
WHERE pending.episode->>'reason'<>'OWNER_CHANGES_REQUESTED'
   OR (SELECT count(*) FROM decisions WHERE root_id=pending.id)=1;
