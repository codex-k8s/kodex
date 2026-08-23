-- name: repository_bootstrap_insert_assistant_core_prompt_audit :exec
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
       'system_assistant.core_prompt_upgraded',
       'SYSTEM_ASSISTANT',
       runtime.stable_key,
       'SUCCEEDED',
       'i18n:SYSTEM_ASSISTANT_CORE_PROMPT_UPDATED',
       'bootstrap'
FROM control_plane.assistant_runtime runtime
JOIN control_plane.subjects subject
  ON subject.organization_id = runtime.organization_id
 AND subject.issuer = 'mattercodex-system'
 AND subject.active
WHERE runtime.organization_id = @organization_id::uuid
  AND runtime.agent_id = @agent_id::uuid;
