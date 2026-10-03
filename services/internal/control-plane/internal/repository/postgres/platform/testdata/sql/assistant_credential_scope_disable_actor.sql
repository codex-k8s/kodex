UPDATE control_plane.subjects actor
SET active = false
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id
JOIN control_plane.runs root_run ON root_run.id = revision.root_run_id
WHERE lease.ref = $1 AND actor.id = root_run.initiated_by
  AND actor.organization_id = revision.organization_id;
