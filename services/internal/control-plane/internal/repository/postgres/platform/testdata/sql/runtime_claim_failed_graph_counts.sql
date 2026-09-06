SELECT
 (SELECT count(*) FROM control_plane.runtime_leases lease JOIN control_plane.runs run ON run.id=lease.run_id WHERE run.ref=$1 AND lease.state='CLAIMED'),
 (SELECT count(*) FROM control_plane.session_turns turn JOIN control_plane.runs run ON run.id=turn.run_id WHERE run.ref=$1 AND turn.state IN ('QUEUED','RUNNING')),
 (SELECT count(*) FROM control_plane.runtime_revisions revision JOIN control_plane.runs run ON run.id=revision.run_id WHERE run.ref=$1);
