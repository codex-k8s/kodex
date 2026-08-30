-- name: repository_bootstrap_insert_assistant_runtime_environment_audit :exec
INSERT INTO control_plane.audit_events(
    ref,
    organization_id,
    actor_id,
    assistant_agent_id,
    action,
    resource_kind,
    resource_ref,
    outcome,
    safe_summary,
    correlation_ref
)
SELECT @audit_ref,
       runtime.organization_id,
       subject.id,
       runtime.agent_id,
       'system_assistant.runtime_environment_reconciled',
       'RUNTIME_ENVIRONMENT',
       environment.ref,
       'SUCCEEDED',
       'i18n:SYSTEM_ASSISTANT_RUNTIME_ENVIRONMENT_UPDATED',
       'bootstrap'
FROM control_plane.assistant_runtime runtime
JOIN control_plane.subjects subject
  ON subject.organization_id = runtime.organization_id
 AND subject.issuer = 'kodex-system'
 AND subject.active
JOIN control_plane.runtime_environment_sets environment
  ON environment.id = @environment_id::uuid
 AND environment.organization_id = runtime.organization_id
WHERE runtime.organization_id = @organization_id::uuid
  AND runtime.agent_id = @agent_id::uuid;
