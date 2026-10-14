-- name: configuration_applyassistantplancommand_promote_session_storage_project :one
WITH target_session AS (
    SELECT session.id, session.organization_id, session.project_id
    FROM control_plane.sessions session
    WHERE session.id = $2::uuid
      AND session.organization_id = $3::uuid
      AND session.project_id = $1::uuid
      AND session.state = 'ACTIVE'
    FOR UPDATE
), session_runs AS (
    SELECT run.id, run.root_run_id
    FROM control_plane.runs run
    JOIN target_session session ON session.id = run.session_id
), session_turns AS (
    SELECT turn.id
    FROM control_plane.session_turns turn
    JOIN target_session session ON session.id = turn.session_id
), run_artifacts AS (
    SELECT artifact.id
    FROM control_plane.artifacts artifact
    JOIN session_runs run ON run.id = artifact.run_id
), boundary AS (
    SELECT EXISTS (SELECT 1 FROM target_session)
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.runs run
           JOIN target_session session ON session.id = run.session_id
           WHERE run.project_id IS NOT NULL AND run.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.run_events event
           JOIN session_runs run ON run.id = event.root_run_id
           CROSS JOIN target_session session
           WHERE event.project_id IS NOT NULL AND event.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.runtime_revisions revision
           JOIN target_session session ON session.id = revision.session_id
           WHERE revision.project_id IS NOT NULL AND revision.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.artifacts artifact
           JOIN session_runs run ON run.id = artifact.run_id
           CROSS JOIN target_session session
           WHERE artifact.project_id IS NOT NULL AND artifact.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.artifact_download_grants download_grant
           JOIN run_artifacts artifact ON artifact.id = download_grant.artifact_id
           CROSS JOIN target_session session
           WHERE download_grant.project_id IS NOT NULL AND download_grant.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.attachment_bindings binding
           CROSS JOIN target_session session
           WHERE (binding.run_id IN (SELECT id FROM session_runs)
                  OR binding.assistant_turn_id IN (SELECT id FROM session_turns)
                  OR binding.session_turn_id IN (SELECT id FROM session_turns))
             AND binding.project_id IS NOT NULL AND binding.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.interaction_message_receipts receipt
           JOIN session_runs run ON run.id = receipt.root_run_id
           CROSS JOIN target_session session
           WHERE receipt.project_id IS NOT NULL AND receipt.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.session_archives archive
           JOIN target_session session ON session.id = archive.session_id
           WHERE archive.project_id IS NOT NULL AND archive.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.session_archive_tasks task
           JOIN target_session session ON session.id = task.session_id
           WHERE task.project_id IS NOT NULL AND task.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.session_storage storage
           JOIN target_session session ON session.id = storage.session_id
           WHERE storage.project_id IS NOT NULL AND storage.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.owner_gates gate
           JOIN session_runs run ON run.id = gate.root_run_id
           CROSS JOIN target_session session
           WHERE gate.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.integration_approval_scopes approval_scope
           JOIN session_runs run ON run.id = approval_scope.root_run_id
           CROSS JOIN target_session session
           WHERE approval_scope.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.interaction_deliveries delivery
           JOIN session_runs run ON run.id = delivery.root_run_id
           CROSS JOIN target_session session
           WHERE delivery.project_id <> session.project_id
       )
       AND NOT EXISTS (
           SELECT 1 FROM control_plane.runtime_file_catalogs catalog
           JOIN target_session session ON session.id = catalog.session_id
           WHERE catalog.project_id <> session.project_id
       ) AS allowed
), promoted_runs AS (
    UPDATE control_plane.runs run
    SET project_id = session.project_id
    FROM target_session session, boundary
    WHERE boundary.allowed
      AND run.session_id = session.id
      AND run.project_id IS NULL
    RETURNING run.id
), promoted_events AS (
    UPDATE control_plane.run_events event
    SET project_id = session.project_id
    FROM session_runs run, target_session session, boundary
    WHERE boundary.allowed
      AND event.root_run_id = run.id
      AND event.project_id IS NULL
    RETURNING event.id
), promoted_revisions AS (
    UPDATE control_plane.runtime_revisions revision
    SET project_id = session.project_id
    FROM target_session session, boundary
    WHERE boundary.allowed
      AND revision.session_id = session.id
      AND revision.project_id IS NULL
    RETURNING revision.id
), promoted_artifacts AS (
    UPDATE control_plane.artifact_heads artifact
    SET project_id = session.project_id
    FROM session_runs run, target_session session, boundary
    WHERE boundary.allowed
      AND artifact.run_id = run.id
      AND artifact.project_id IS NULL
    RETURNING artifact.id
), promoted_download_grants AS (
    UPDATE control_plane.artifact_download_grants download_grant
    SET project_id = session.project_id
    FROM run_artifacts artifact, target_session session, boundary
    WHERE boundary.allowed
      AND download_grant.artifact_id = artifact.id
      AND download_grant.project_id IS NULL
    RETURNING download_grant.id
), promoted_bindings AS (
    UPDATE control_plane.attachment_bindings binding
    SET project_id = session.project_id
    FROM target_session session, boundary
    WHERE boundary.allowed
      AND (binding.run_id IN (SELECT id FROM session_runs)
           OR binding.assistant_turn_id IN (SELECT id FROM session_turns)
           OR binding.session_turn_id IN (SELECT id FROM session_turns))
      AND binding.project_id IS NULL
    RETURNING binding.ref
), promoted_receipts AS (
    UPDATE control_plane.interaction_message_receipts receipt
    SET project_id = session.project_id
    FROM session_runs run, target_session session, boundary
    WHERE boundary.allowed
      AND receipt.root_run_id = run.id
      AND receipt.project_id IS NULL
    RETURNING receipt.id
), promoted_archives AS (
    UPDATE control_plane.session_archives archive
    SET project_id = session.project_id
    FROM target_session session, boundary
    WHERE boundary.allowed
      AND archive.session_id = session.id
      AND archive.project_id IS NULL
    RETURNING archive.id
), promoted_archive_tasks AS (
    UPDATE control_plane.session_archive_tasks task
    SET project_id = session.project_id
    FROM target_session session, boundary
    WHERE boundary.allowed
      AND task.session_id = session.id
      AND task.project_id IS NULL
    RETURNING task.id
), promoted_storage AS (
    UPDATE control_plane.session_storage storage
    SET project_id = session.project_id,
        version = storage.version + 1,
        updated_at = clock_timestamp()
    FROM target_session session, boundary
    WHERE boundary.allowed
      AND storage.session_id = session.id
      AND storage.organization_id = session.organization_id
      AND storage.project_id IS NULL
    RETURNING storage.session_id
)
SELECT allowed FROM boundary;
