-- PF-фрагмент для canonical project purge. Не отдельный writer/endpoint:
-- canonical caller сначала проверяет project ownership, закрывает граф и входит
-- в существующий project_purge_authorized контекст. Parameters — его server IDs.
-- Сначала lossfacts с полным tenant/stream/incarnation join; затем bindings и rows.
DELETE FROM control_plane.kubernetes_capture_losses loss
USING control_plane.kubernetes_port_forward_connections connection,
 control_plane.kubernetes_execution_streams stream,
 control_plane.kubernetes_execution_effects effect,
 control_plane.kubernetes_execution_children child,
 control_plane.kubernetes_execution_plans plan,
 control_plane.integration_invocations invocation
WHERE invocation.project_id=@project_id::uuid AND invocation.organization_id=@organization_id::uuid
 AND plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
 AND child.plan_id=plan.id AND child.organization_id=plan.organization_id
 AND effect.child_id=child.id AND effect.organization_id=child.organization_id
 AND stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
 AND connection.stream_id=stream.id AND connection.organization_id=stream.organization_id
 AND loss.source_ref=connection.ref AND loss.source_generation=connection.generation
 AND loss.stream_id=connection.stream_id AND loss.organization_id=connection.organization_id;

DELETE FROM control_plane.kubernetes_port_forward_frames binding
USING control_plane.kubernetes_port_forward_connections connection,
 control_plane.kubernetes_execution_streams stream,
 control_plane.kubernetes_execution_effects effect,
 control_plane.kubernetes_execution_children child,
 control_plane.kubernetes_execution_plans plan,
 control_plane.integration_invocations invocation
WHERE invocation.project_id=@project_id::uuid AND invocation.organization_id=@organization_id::uuid
 AND plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
 AND child.plan_id=plan.id AND child.organization_id=plan.organization_id
 AND effect.child_id=child.id AND effect.organization_id=child.organization_id
 AND stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
 AND connection.stream_id=stream.id AND connection.organization_id=stream.organization_id
 AND binding.connection_id=connection.id AND binding.stream_id=connection.stream_id
 AND binding.organization_id=connection.organization_id;

DELETE FROM control_plane.kubernetes_port_forward_connections connection
USING control_plane.kubernetes_execution_streams stream,
 control_plane.kubernetes_execution_effects effect,
 control_plane.kubernetes_execution_children child,
 control_plane.kubernetes_execution_plans plan,
 control_plane.integration_invocations invocation
WHERE invocation.project_id=@project_id::uuid AND invocation.organization_id=@organization_id::uuid
 AND plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
 AND child.plan_id=plan.id AND child.organization_id=plan.organization_id
 AND effect.child_id=child.id AND effect.organization_id=child.organization_id
 AND stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
 AND connection.stream_id=stream.id AND connection.organization_id=stream.organization_id;
