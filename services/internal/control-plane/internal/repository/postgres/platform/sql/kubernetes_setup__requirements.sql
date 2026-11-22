-- name: kubernetes_setup__requirements :many
SELECT connection.ref,connection.version,connection.credential_policy,target.ref,target.revision,
 target.payload::jsonb->'target'->>'embeddedContext'
FROM control_plane.integration_connections connection
JOIN control_plane.kubernetes_operator_current current ON true
JOIN control_plane.kubernetes_operator_registry_revisions registry ON registry.revision=current.revision
CROSS JOIN LATERAL jsonb_array_elements(registry.payload::jsonb->'targets') entry
JOIN control_plane.kubernetes_target_revisions target ON target.ref=entry->'target'->>'ref'
 AND target.revision=(entry->'target'->>'revision')::bigint
WHERE connection.organization_id=@organization_id::uuid AND connection.ref=ANY(@connection_refs::text[])
 AND connection.lifecycle_state='ACTIVE' AND connection.enabled AND connection.definition_key='kubernetes-managed'
 AND connection.credential_policy IN('INITIATOR_REQUIRED','PROJECT_SHARED')
 AND target.ref=connection.public_configuration->>'target_ref';
