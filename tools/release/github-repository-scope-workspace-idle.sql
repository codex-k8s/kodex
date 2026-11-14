-- ACL-only maintenance не меняет workspace rows, filesystem, writer custody
-- или quiescence. Historical BLOCKED/PENDING records сохраняются без трактовки
-- как released. Закрывается только реально активный owner work/grant graph.
SELECT NOT EXISTS(SELECT 1 FROM control_plane.runs WHERE state NOT IN('SUCCEEDED','FAILED','CANCELLED'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.run_nodes WHERE state IN('QUEUED','RUNNING','WAITING'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.runtime_leases WHERE state='CLAIMED')
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_archive_tasks WHERE state IN('READY','CLAIMED'))
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_tasks WHERE state IN('READY','CLAIMED'))
 -- Даже expired ACTIVE grant не считается CLOSED по времени.
 AND NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_volume_attempts WHERE grant_state='ACTIVE');
