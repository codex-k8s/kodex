-- name: secret_draft_impact_get :one
SELECT p.id::text,p.ref,d.ref,p.draft_version,s.ref,p.secret_version,p.source_revision,p.digest,
CASE WHEN p.state='PREPARED' AND p.expires_at<=clock_timestamp() THEN 'EXPIRED' ELSE p.state END,
p.expires_at,p.intent_digest,COALESCE(p.operation_id::text,''),count(i.ref)
FROM control_plane.runtime_secret_draft_impact_plans p
JOIN control_plane.runtime_secret_drafts d ON d.id=p.draft_id
JOIN control_plane.runtime_secrets s ON s.id=d.secret_id
LEFT JOIN control_plane.runtime_secret_draft_impact_items i ON i.plan_id=p.id
WHERE p.organization_id=@organization_id::uuid AND p.actor_id=@actor_id::uuid
AND ((@plan_ref<>'' AND p.ref=@plan_ref) OR (@idempotency_key<>'' AND p.idempotency_key=@idempotency_key)
OR (@operation_id<>'' AND p.operation_id=NULLIF(@operation_id,'')::uuid))
GROUP BY p.id,d.ref,s.ref;
