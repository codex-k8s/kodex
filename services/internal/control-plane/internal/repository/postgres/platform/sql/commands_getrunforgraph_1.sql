-- name: platform__commands_getrunforgraph_1 :one
SELECT r.ref,COALESCE(p.ref,''),s.ref,root.ref,COALESCE(parent.ref,''),COALESCE(retry.ref,''),r.title,r.task,r.state,r.source,r.result_summary,r.safe_error_code,r.safe_error_message,sub.display_name,r.target_type,r.target_ref,COALESCE(a.name,w.name,sa.name,r.target_ref),r.attempt,r.graph_revision,r.event_sequence,r.version,r.input,r.input_artifact_refs,r.created_at,r.started_at,r.finished_at
FROM control_plane.runs r
LEFT JOIN control_plane.projects p ON p.id=r.project_id
JOIN control_plane.sessions s ON s.id=r.session_id
JOIN control_plane.runs root ON root.id=r.root_run_id
LEFT JOIN control_plane.runs parent ON parent.id=r.parent_run_id
LEFT JOIN control_plane.runs retry ON retry.id=r.retry_of_run_id
JOIN control_plane.subjects sub ON sub.id=r.initiated_by
LEFT JOIN control_plane.agents a ON r.target_type IN ('AGENT','SYSTEM_ASSISTANT') AND a.ref=r.target_ref
LEFT JOIN control_plane.workflows w ON r.target_type='WORKFLOW' AND w.ref=r.target_ref
LEFT JOIN control_plane.agents sa ON r.target_type='SYSTEM_ASSISTANT' AND sa.system_key='system-assistant'
WHERE r.organization_id=$1::uuid AND r.ref=$2
