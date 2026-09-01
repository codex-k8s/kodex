-- name: mvp_list_schedule_runs :many
SELECT schedule.ref, revision.ref, revision.revision,
       run.ref, COALESCE(project.ref, ''), session.ref, root.ref, COALESCE(parent.ref, ''),
       COALESCE(retry.ref, ''), run.title, run.title_source,
       COALESCE(run.presentation_metadata->>'activitySummary', ''), run.task, run.state, run.source,
       run.result_summary, run.safe_error_code, run.safe_error_message, subject.display_name,
       run.target_type, run.target_ref, COALESCE(agent.name, workflow.name, assistant.name, run.target_ref),
       run.attempt, run.graph_revision, run.event_sequence, run.version, run.input,
       COALESCE(input_attachment_set.ref, ''),
       COALESCE((SELECT array_agg(artifact.ref ORDER BY artifact.created_at)
                 FROM control_plane.artifacts artifact
                 JOIN control_plane.runs artifact_run ON artifact_run.id = artifact.run_id
                 WHERE artifact_run.root_run_id = run.root_run_id), '{}'::text[]),
       COALESCE((SELECT array_agg(gate.ref ORDER BY gate.created_at)
                 FROM control_plane.owner_gates gate WHERE gate.root_run_id = run.root_run_id), '{}'::text[]),
       run.usage, run.created_at, run.started_at, run.finished_at
FROM control_plane.schedule_occurrences occurrence
JOIN control_plane.schedules schedule ON schedule.id = occurrence.schedule_id
JOIN control_plane.schedule_revisions revision ON revision.id = occurrence.schedule_revision_id
JOIN control_plane.runs run ON run.id = occurrence.run_id
LEFT JOIN control_plane.projects project ON project.id = run.project_id
JOIN control_plane.sessions session ON session.id = run.session_id
JOIN control_plane.runs root ON root.id = run.root_run_id
LEFT JOIN control_plane.runs parent ON parent.id = run.parent_run_id
LEFT JOIN control_plane.runs retry ON retry.id = run.retry_of_run_id
JOIN control_plane.subjects subject ON subject.id = run.initiated_by
LEFT JOIN control_plane.agents agent ON run.target_type IN ('AGENT', 'SYSTEM_ASSISTANT') AND agent.ref = run.target_ref
LEFT JOIN control_plane.workflows workflow ON run.target_type = 'WORKFLOW' AND workflow.ref = run.target_ref
LEFT JOIN control_plane.agents assistant ON run.target_type = 'SYSTEM_ASSISTANT' AND assistant.system_key = 'system-assistant'
LEFT JOIN control_plane.attachment_sets input_attachment_set ON input_attachment_set.id = run.input_attachment_set_id
WHERE schedule.organization_id = @organization_id::uuid
  AND schedule.ref = @schedule_ref
  AND (@cursor_ref = '' OR (run.created_at, run.ref) < (
      SELECT cursor.created_at, cursor.ref FROM control_plane.runs cursor
      WHERE cursor.organization_id = @organization_id::uuid AND cursor.ref = @cursor_ref
  ))
ORDER BY run.created_at DESC, run.ref DESC
LIMIT @page_size;
