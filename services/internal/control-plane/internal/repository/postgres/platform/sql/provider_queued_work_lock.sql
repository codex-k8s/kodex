-- name: provider_queued_work_lock :one
SELECT run.version, run.state,
       EXISTS (SELECT 1 FROM control_plane.provider_account_blockers(@organization_id::uuid,@account_id::uuid) blocker
               WHERE blocker.kind='QUEUED_TURN' AND blocker.ref=run.ref),
       control_plane.provider_queued_run_cancellable(@organization_id::uuid,run.id)
FROM control_plane.runs run
WHERE run.organization_id=@organization_id::uuid AND run.ref=@run_ref
FOR UPDATE OF run;
