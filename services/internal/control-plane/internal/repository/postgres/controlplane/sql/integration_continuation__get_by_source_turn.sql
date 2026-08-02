SELECT id, organization_id, project_id, process_id, session_id,
       session_version, thread_id, role_id, turn_id, turn_version, attempt,
       runtime_revision_id, runtime_revision_version, runtime_revision_sha256,
       immutable_input_sha256, grant_generation, invocation_id, approval_id,
       integration_id, integration_version, integration_sha256,
       credential_bindings, request_sha256, approval_state, execution_state,
       continuation_state, version, fence, approval_expires_at,
       coalesce(decision_reference, ''), coalesce(decision_sha256, ''),
       coalesce(result_reference, ''), coalesce(result_sha256, ''),
       coalesce(error_code, ''), coalesce(error_reference, ''),
       coalesce(error_sha256, ''), coalesce(continuation_turn_id::text, ''),
       coalesce(continuation_turn_version, 0),
       coalesce(continuation_attempt, 0),
       coalesce(continuation_runtime_revision_id::text, ''),
       coalesce(continuation_runtime_revision_version, 0),
       coalesce(continuation_input_sha256, ''), created_at, updated_at
FROM control_plane.integration_continuations
WHERE turn_id = @turn_id
ORDER BY created_at DESC, id
LIMIT 1
FOR UPDATE;
