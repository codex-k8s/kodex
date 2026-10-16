-- name: project_purge__subject_artifact_owner :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.artifact_history artifact
    JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
    JOIN control_plane.runs run ON run.id=artifact.run_id AND run.organization_id=artifact.organization_id
    JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=run.organization_id
    JOIN control_plane.assistant_conversations conversation
      ON conversation.session_id=session.id AND conversation.organization_id=session.organization_id
    JOIN control_plane.projects project ON project.id=artifact.project_id AND project.organization_id=artifact.organization_id
    JOIN control_plane.organizations organization ON organization.id=artifact.organization_id
    JOIN control_plane.subjects actor ON actor.id=artifact.created_by AND actor.organization_id=artifact.organization_id
    WHERE artifact.organization_id=@organization_id::uuid AND artifact.project_id=@project_id::uuid
      AND run.project_id=project.id AND session.project_id=project.id AND conversation.project_id=project.id
      AND conversation.assistant_scope='SYSTEM' AND project.ref=@project_ref AND project.lifecycle='PURGE_PENDING'
      AND content.object_key=@object_key AND @object_version<>'' AND content.object_version=@object_version
      AND starts_with(content.object_key,'organizations/' || organization.ref || '/subjects/' || actor.ref || '/artifacts/' || artifact.ref || '/')
);
