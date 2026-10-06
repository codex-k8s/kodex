-- name: role_images_get_admission_terminal :one
SELECT attempt.state, attempt.version, attempt.fence,
       (attempt.terminal_artifact_json->>'version')::bigint
FROM control_plane.image_admission_attempts attempt
JOIN control_plane.image_artifacts artifact ON artifact.id=attempt.artifact_id
JOIN control_plane.image_builds build ON build.id=artifact.build_id
JOIN control_plane.role_image_recipes recipe ON recipe.id=artifact.recipe_id
JOIN control_plane.organizations organization ON organization.id=artifact.organization_id
LEFT JOIN control_plane.projects project ON project.id=artifact.project_id
LEFT JOIN control_plane.image_admission_risk_decisions decision ON decision.id=attempt.risk_decision_id
WHERE attempt.organization_id=@organization_id::uuid AND artifact.organization_id=@organization_id::uuid
  AND artifact.ref=@artifact_ref AND attempt.ref=@attempt_ref AND attempt.number=@attempt
  AND attempt.state IN ('ACCEPTED','REJECTED','FAILED','CANCELLED') AND attempt.finished_at IS NOT NULL AND attempt.version>1
  AND attempt.source_admission_revision=@source_admission_revision
  AND attempt.source_receipt_sha256=@source_receipt_sha256
  AND attempt.source_evidence_manifest_digest=@source_evidence_manifest_digest
  AND COALESCE(decision.risk_acceptance_sha256,'')=@risk_acceptance_sha256
  AND artifact.scope_kind=@scope_kind AND organization.ref=@organization_ref AND COALESCE(project.ref,'')=@project_ref
  AND build.ref=@build_ref
  AND recipe.ref=@recipe_ref
  AND attempt.terminal_artifact_json->>'id'=artifact.id::text
  AND attempt.terminal_artifact_json->>'organization_id'=artifact.organization_id::text
  AND attempt.terminal_artifact_json->>'scope_kind'=@scope_kind
  AND COALESCE(attempt.terminal_artifact_json->>'project_id','')=COALESCE(artifact.project_id::text,'')
  AND attempt.terminal_artifact_json->>'build_id'=build.id::text
  AND attempt.terminal_artifact_json->>'recipe_id'=recipe.id::text
  AND (attempt.terminal_artifact_json->>'build_attempt')::integer=@build_attempt
  AND (attempt.terminal_artifact_json->>'recipe_generation')::bigint=@recipe_generation
  AND attempt.terminal_artifact_json->>'manifest_digest'=@manifest_digest
  AND attempt.terminal_artifact_json->>'immutable_build_sha256'=@immutable_build_sha256
  AND attempt.terminal_artifact_json->>'provenance_sha256'=@provenance_sha256
  AND (attempt.terminal_artifact_json->>'policy_revision')::bigint=@policy_revision
  AND attempt.terminal_artifact_json->>'policy_sha256'=@policy_sha256
  AND attempt.terminal_artifact_json->>'spec_sha256'=@spec_sha256
  AND (attempt.terminal_artifact_json->>'version')::bigint>@claim_version
  AND attempt.fence>=@claim_fence
  AND (attempt.terminal_artifact_json->>'admission_fence')::bigint=attempt.fence
  AND attempt.terminal_artifact_json->>'admission_state'=CASE WHEN attempt.state='CANCELLED' THEN 'REJECTED' ELSE attempt.state END
  AND (attempt.state<>'CANCELLED' OR attempt.terminal_artifact_json->>'admission_verdict'='')
  AND attempt.terminal_artifact_json->>'admission_claim_token_sha256' IS NULL
  AND attempt.terminal_artifact_json->>'admission_claimant_workload' IS NULL
  AND (attempt.terminal_artifact_json->>'admission_authority_generation')::bigint=0
