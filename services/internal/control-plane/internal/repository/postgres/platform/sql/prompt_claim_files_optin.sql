-- name: prompt_claim_files_optin :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.artifact_bindings binding
    JOIN control_plane.artifacts artifact ON artifact.id=binding.artifact_id
    WHERE binding.target_kind='KNOWLEDGE' AND binding.target_ref=@agent_ref
      AND artifact.organization_id=@organization_id::uuid
      AND artifact.project_id IS NOT DISTINCT FROM NULLIF(@project_id,'')::uuid
      AND artifact.lifecycle_state='ACTIVE'
) OR EXISTS (
    SELECT 1 FROM control_plane.session_turns turn
    JOIN control_plane.attachment_sets attachment ON attachment.id=turn.attachment_set_id
      AND attachment.organization_id=turn.organization_id AND attachment.state='FINALIZED'
    WHERE turn.organization_id=@organization_id::uuid AND turn.session_id=@session_id::uuid
);
