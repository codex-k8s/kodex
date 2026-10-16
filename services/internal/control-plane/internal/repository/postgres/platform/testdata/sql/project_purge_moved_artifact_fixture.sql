-- Readback результата настоящего CompleteExecution в disposable fixture.
SELECT content.object_key,content.object_version
FROM control_plane.artifact_history artifact
JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
JOIN control_plane.runs run ON run.id=artifact.run_id AND run.organization_id=artifact.organization_id
JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=run.organization_id
WHERE session.ref=$1 AND artifact.source='AGENT_RESULT'
ORDER BY artifact.created_at DESC LIMIT 1;
