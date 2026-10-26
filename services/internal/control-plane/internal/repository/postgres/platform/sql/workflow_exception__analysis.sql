-- name: workflow_exception__analysis :one
SELECT jsonb_build_object('stepKey',node.workflow_step_key,'runtimeState',node.state,
    'modelResultData',node.progress_summary,'safeErrorCode',node.safe_error_code,
    'capturedArtifactMetadata',COALESCE(node.workflow_captured_artifacts->'artifacts','[]'::jsonb))
FROM control_plane.run_nodes node
WHERE node.organization_id=@organization_id::uuid AND node.root_run_id=@root_run_id::uuid AND node.id=@node_id::uuid;
