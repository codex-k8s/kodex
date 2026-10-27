-- Только DDL отдельной disposable-копии; прежний CHECK несовместим с SUPERSEDED.
ALTER TABLE control_plane.owner_gates DROP CONSTRAINT owner_gates_resolution_check;
ALTER TABLE control_plane.owner_gates ADD CONSTRAINT owner_gates_resolution_check CHECK (
        (state='OPEN' AND decision IS NULL AND resolved_by IS NULL AND resolved_at IS NULL) OR
        (state='EXPIRED' AND decision IS NULL AND resolved_by IS NULL AND resolved_at IS NOT NULL
         AND expires_at IS NOT NULL AND integration_invocation_id IS NULL) OR
        (state IN ('APPROVED','REJECTED','CHANGES_REQUESTED','CANCELLED')
         AND decision IS NOT NULL AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL));
