-- name: telegram_list_deliveries :many
SELECT delivery.ref,delivery.version,connection.ref,run.ref,COALESCE(gate.ref,''),
 COALESCE(delivery.telegram_input->>'event_type',''),delivery.state,delivery.attempt,
 delivery.state='FAILED' AND delivery.attempt<delivery.execution_max_attempts AND delivery.telegram_effect_started_at IS NULL,
 delivery.safe_error_code,delivery.created_at,delivery.available_at,delivery.completed_at,
 CASE WHEN delivery.telegram_receipt IS NOT NULL THEN delivery.ref ELSE '' END
FROM control_plane.interaction_deliveries delivery
JOIN control_plane.runs run ON run.id=delivery.root_run_id AND run.organization_id=delivery.organization_id
JOIN control_plane.integration_connections connection ON connection.id=delivery.connection_id AND connection.organization_id=delivery.organization_id
LEFT JOIN control_plane.owner_gates gate ON gate.id=COALESCE(delivery.telegram_gate_id,delivery.gate_id)
WHERE delivery.organization_id=@organization_id::uuid AND delivery.project_id=@project_id::uuid
 AND (@after='' OR (delivery.created_at,delivery.ref)<(SELECT anchor.created_at,anchor.ref
  FROM control_plane.interaction_deliveries anchor WHERE anchor.organization_id=@organization_id::uuid
   AND anchor.project_id=@project_id::uuid AND anchor.ref=@after))
 AND control_plane.catalog_resource_visible(connection.organization_id,@actor_id::uuid,'integration.manage','INTEGRATION',
  connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())
 AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
  WHERE target.organization_id=run.organization_id AND target.kind='RUN' AND target.id=run.id
   AND control_plane.catalog_resource_visible(run.organization_id,@actor_id::uuid,'run.view',target.kind,
    target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp()))
ORDER BY delivery.created_at DESC,delivery.ref DESC LIMIT @limit;
