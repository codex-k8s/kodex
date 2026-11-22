-- name: kubernetes_project__close_captures :exec
WITH streams AS MATERIALIZED (
 SELECT stream.id,stream.organization_id FROM control_plane.kubernetes_execution_streams stream
 JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
 JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
 JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
 JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=plan.invocation_id AND pin.organization_id=plan.organization_id
 JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
 WHERE stream.organization_id=@organization_id::uuid AND root.project_id=@project_id::uuid
 ORDER BY stream.id FOR UPDATE OF stream
), captures AS MATERIALIZED (
 SELECT capture.id,capture.organization_id FROM control_plane.kubernetes_output_captures capture
 JOIN streams ON streams.id=capture.stream_id AND streams.organization_id=capture.organization_id
 ORDER BY capture.id FOR UPDATE OF capture
), closed_attempts AS (
 UPDATE control_plane.kubernetes_capture_attempts attempt SET state='CANCELLED',completed_at=clock_timestamp()
 FROM captures WHERE attempt.capture_id=captures.id AND attempt.organization_id=captures.organization_id AND attempt.state='ACTIVE'
 RETURNING attempt.id
)
UPDATE control_plane.kubernetes_output_captures capture SET state='WAITING_OWNER',reason_code='CAPTURE_ELIGIBILITY_CHANGED'
FROM captures WHERE capture.id=captures.id AND capture.organization_id=captures.organization_id
 AND capture.state IN('READY','CLAIMED','UNKNOWN','WAITING_OWNER');
