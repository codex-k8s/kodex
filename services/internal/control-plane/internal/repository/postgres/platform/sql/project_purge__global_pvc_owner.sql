-- name: project_purge__global_pvc_owner :one
-- Та же граница SYSTEM-session promotion, что у runtime-controller: session
-- и conversation уже принадлежат frozen проекту, прежний PVC ещё global.
SELECT EXISTS (
    SELECT 1 FROM control_plane.sessions session
    JOIN control_plane.assistant_conversations conversation
      ON conversation.session_id=session.id AND conversation.organization_id=session.organization_id
    JOIN control_plane.projects project
      ON project.id=session.project_id AND project.organization_id=session.organization_id
    WHERE session.organization_id=@organization_id::uuid AND session.project_id=@project_id::uuid
      AND conversation.project_id=project.id AND conversation.assistant_scope='SYSTEM'
      AND project.ref=@project_ref AND project.lifecycle='PURGE_PENDING' AND session.ref=@session_ref
);
