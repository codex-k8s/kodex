-- name: runtime_deadline_proof :one
SELECT run.execution_started_at,run.execution_deadline_at,run.execution_timeout_seconds,run.execution_step_key,
       (SELECT count(*) FROM control_plane.runtime_leases lease WHERE lease.run_id IN (
           SELECT id FROM control_plane.runs WHERE root_run_id=run.root_run_id) AND lease.state='CLAIMED'),
       (SELECT count(*) FROM control_plane.run_nodes node WHERE node.root_run_id=run.root_run_id AND node.state IN ('PLANNED','QUEUED','RUNNING','WAITING')),
       control_plane.runtime_execution_before_deadline(run.organization_id,run.id)
FROM control_plane.runs run WHERE run.ref=@run_ref;
