-- name: prompt_continuation_pin_turn :exec
UPDATE control_plane.session_turns turn
SET expected_prompt_context_digest=@context_digest,
    expected_prompt_dependency_digest=@dependency_digest
FROM control_plane.runs run
WHERE turn.organization_id=@organization_id::uuid AND turn.run_id=run.id
  AND run.organization_id=turn.organization_id AND run.ref=@run_ref
  AND turn.expected_prompt_context_digest='' AND turn.expected_prompt_dependency_digest='';
