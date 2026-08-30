-- name: commands_getrunforgraph_select_runs_organization_id_ref :one
SELECT r.ref,COALESCE(p.ref,''),s.ref,root.ref,COALESCE(parent.ref,''),COALESCE(retry.ref,''),r.title,r.title_source,COALESCE(r.presentation_metadata->>'activitySummary',''),r.task,r.state,r.source,r.result_summary,r.safe_error_code,r.safe_error_message,sub.display_name,r.target_type,r.target_ref,COALESCE(a.name,w.name,sa.name,r.target_ref),r.attempt,r.graph_revision,r.event_sequence,r.version,r.input,COALESCE(input_attachment_set.ref,''),
       COALESCE((SELECT array_agg(artifact.ref ORDER BY artifact.created_at) FROM control_plane.artifacts artifact JOIN control_plane.runs artifact_run ON artifact_run.id=artifact.run_id WHERE artifact_run.root_run_id=r.root_run_id),'{}'::text[]),
       COALESCE((SELECT array_agg(gate.ref ORDER BY gate.created_at) FROM control_plane.owner_gates gate WHERE gate.root_run_id=r.root_run_id),'{}'::text[]),
       r.usage,r.created_at,r.started_at,r.finished_at
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
LEFT JOIN control_plane.attachment_sets input_attachment_set ON input_attachment_set.id=r.input_attachment_set_id
WHERE r.organization_id=$1::uuid AND r.ref=$2
