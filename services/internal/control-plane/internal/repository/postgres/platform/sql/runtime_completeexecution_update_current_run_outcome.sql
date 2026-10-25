-- name: runtime_completeexecution_update_current_run_outcome :exec
UPDATE control_plane.runs target
SET state=CASE WHEN $2='SUCCEEDED' AND (
      EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_run_id=target.id AND launch.state='OPEN')
      OR control_plane.repository_bundle_callback_pending(target.organization_id,target.id)) THEN 'RUNNING' ELSE $2 END,
    result_summary=$3,safe_error_code=$4,safe_error_message=$5,
    finished_at=CASE WHEN $2='SUCCEEDED' AND (
      EXISTS(SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.origin_run_id=target.id AND launch.state='OPEN')
      OR control_plane.repository_bundle_callback_pending(target.organization_id,target.id)) THEN NULL ELSE clock_timestamp() END,
    version=version+1,updated_at=clock_timestamp()
WHERE target.id=$1::uuid AND target.id<>target.root_run_id
