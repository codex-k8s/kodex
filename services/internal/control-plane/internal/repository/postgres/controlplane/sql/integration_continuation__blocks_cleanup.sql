-- name: IntegrationContinuationBlocksCleanup
SELECT EXISTS (
    SELECT 1
    FROM control_plane.integration_continuations
    WHERE organization_id = @organization_id::uuid
      AND project_id = @project_id::uuid
      AND session_id = @session_id::uuid
      AND continuation_state <> 'REJOINED'
);
