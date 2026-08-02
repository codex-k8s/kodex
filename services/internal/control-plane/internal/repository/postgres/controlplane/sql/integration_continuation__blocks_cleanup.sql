-- name: IntegrationContinuationBlocksCleanup
SELECT EXISTS (
    SELECT 1
    FROM control_plane.integration_continuations
    WHERE turn_id = @turn_id::uuid
      AND attempt = @attempt
      AND continuation_state <> 'REJOINED'
);
