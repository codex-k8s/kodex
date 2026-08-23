-- name: commands_changeworkflow_archive_workflow :exec
UPDATE control_plane.workflows w SET state='ARCHIVED',version=version+1,updated_at=clock_timestamp() WHERE w.id=$1::uuid AND NOT EXISTS(SELECT 1 FROM control_plane.runs r WHERE r.target_type='WORKFLOW' AND r.target_ref=w.ref AND r.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING'))
