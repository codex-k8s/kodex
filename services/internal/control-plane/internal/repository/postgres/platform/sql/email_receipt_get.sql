-- name: EmailReceiptGet :one
SELECT e.id::text,e.ref,e.version,i.ref,e.external_receipt_ref,e.external_receipt_digest,
       e.semantic_input_digest,e.effect_key,e.outcome,e.mailbox_ref,e.configuration_revision,
       c.ref,p.ref,e.created_at,e.updated_at,r.ref,p.id::text,i.state
FROM control_plane.email_effect_receipts e
JOIN control_plane.integration_invocations i ON i.id=e.invocation_id AND i.organization_id=e.organization_id
JOIN control_plane.integration_connections c ON c.id=i.connection_id AND c.organization_id=e.organization_id
JOIN control_plane.runs r ON r.id=i.run_id AND r.organization_id=e.organization_id
JOIN control_plane.projects p ON p.id=r.project_id AND p.organization_id=e.organization_id
WHERE e.organization_id=@organization_id::uuid
  AND ((@receipt_ref::text<>'' AND e.ref=@receipt_ref) OR (@invocation_ref::text<>'' AND i.ref=@invocation_ref))
FOR UPDATE OF e;
