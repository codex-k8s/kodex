-- name: kubernetes_invocation__pin_matches :one
SELECT EXISTS(SELECT 1 FROM control_plane.kubernetes_invocation_identity_bindings pin
 WHERE pin.invocation_id=@invocation_id::uuid AND pin.organization_id=@organization_id::uuid
 AND pin.connection_id=@connection_id::uuid AND pin.root_run_id=@root_id::uuid AND pin.root_actor_id=@root_actor_id::uuid
 AND pin.subject_id=@subject_id::uuid AND pin.binding_id IS NOT DISTINCT FROM NULLIF(@binding_id,'')::uuid
 AND pin.binding_generation=@binding_generation AND pin.identity_revision_id=@identity_id::uuid
 AND pin.credential_policy=@policy
 -- Повторная успешная проверка не заменяет original health lineage: обе observations
 -- должны подтверждать тот же subject и exact immutable source.
 AND EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_health_completions original
  JOIN control_plane.kubernetes_identity_health_attempts attempt ON attempt.id=original.attempt_id
  WHERE original.attempt_id=pin.health_attempt_id AND attempt.organization_id=pin.organization_id
   AND original.outcome='AUTHENTICATED' AND original.applied AND original.state='SUCCEEDED'
   AND original.subject_digest=pin.subject_digest AND attempt.identity_revision_id=pin.identity_revision_id
   AND attempt.registry_revision=pin.registry_revision AND attempt.target_ref=pin.target_ref AND attempt.target_revision=pin.target_revision)
 AND EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_health_completions current
  JOIN control_plane.kubernetes_identity_health_attempts current_attempt ON current_attempt.id=current.attempt_id
  WHERE current.attempt_id=@health_id::uuid AND current_attempt.organization_id=pin.organization_id
   AND current.subject_digest=pin.subject_digest AND current.outcome='AUTHENTICATED' AND current.applied AND current.state='SUCCEEDED')
 AND pin.definition_digest=@definition_digest AND pin.configuration_digest=@configuration_digest
 AND pin.registry_revision=@registry_revision AND pin.target_ref=@target_ref AND pin.target_revision=@target_revision
 AND pin.subject_digest=@subject_digest);
