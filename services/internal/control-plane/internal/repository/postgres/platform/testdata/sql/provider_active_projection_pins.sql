SELECT revision.organization_id::text,run.initiated_by::text,revision.project_id::text,
 lease.ref,lease.workload_instance,lease.generation,revision.ref,revision.revision_digest,
 revision.attempt,revision.input_digest,session.ref,turn.ref
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
JOIN control_plane.runs run ON run.id=revision.root_run_id
JOIN control_plane.sessions session ON session.id=revision.session_id
JOIN control_plane.session_turns turn ON turn.id=revision.turn_id
WHERE lease.ref=$1;
