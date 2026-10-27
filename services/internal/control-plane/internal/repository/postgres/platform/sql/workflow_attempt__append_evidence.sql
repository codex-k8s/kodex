-- name: workflow_attempt__append_evidence :one
WITH completed AS (
    SELECT physical.id,physical.organization_id,physical.root_run_id,physical.turn_id,physical.attempt,
           COALESCE(physical.workflow_step_origin_node_id,physical.id) AS origin_id,
           physical.workflow_business_attempt,run.ref AS run_ref,revision.id AS revision_id,
           lease.id AS lease_id,lease.generation
    FROM control_plane.run_nodes physical
    JOIN control_plane.runs run ON run.id=physical.run_id AND run.organization_id=physical.organization_id
        AND run.root_run_id=physical.root_run_id
    JOIN control_plane.runtime_revisions revision ON revision.node_id=physical.id
        AND revision.organization_id=physical.organization_id AND revision.run_id=physical.run_id
        AND revision.root_run_id=physical.root_run_id AND revision.turn_id=physical.turn_id
        AND revision.agent_id=physical.agent_id AND revision.attempt=physical.attempt
    JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
        AND lease.organization_id=revision.organization_id AND lease.run_id=revision.run_id
        AND lease.node_id=revision.node_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
    WHERE physical.organization_id=@organization_id::uuid AND physical.id=@node_id::uuid
        AND physical.root_run_id=@root_run_id::uuid AND physical.state IN ('SUCCEEDED','FAILED')
        AND revision.id=@runtime_revision_id::uuid AND lease.id=@lease_id::uuid
        AND physical.workflow_business_attempt BETWEEN 1 AND 4
        AND physical.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
        AND physical.workflow_captured_artifacts->>'leaseId'=lease.id::text
), outcome AS (
    SELECT completed.origin_id,completed.organization_id,completed.workflow_business_attempt,
           jsonb_build_object(
               'attempt',completed.workflow_business_attempt,'runRef',completed.run_ref,
               'runtimeRevisionId',completed.revision_id::text,'accepted',@accepted::boolean,
               'reason',@reason::text,'results','[]'::jsonb,'reviewEvidence',@review_evidence::jsonb,
               'provenance',jsonb_build_object(
                   'rootRunId',completed.root_run_id::text,'originNodeId',completed.origin_id::text,
                   'nodeId',completed.id::text,'leaseId',completed.lease_id::text,
                   'generation',completed.generation,'turnId',completed.turn_id::text,
                   'runtimeAttempt',completed.attempt
               )
           ) || CASE WHEN version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
               THEN jsonb_build_object('reworkSubjects',(
                   SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
                       'requirementKey',requirement.value->>'Key','subjectStepKey',requirement.value->>'SubjectStepKey',
                       'subjectResultKey',NULLIF(requirement.value->>'SubjectResultKey',''))))
                   FROM jsonb_array_elements(version.spec->'Steps') step(value)
                   CROSS JOIN LATERAL jsonb_array_elements(step.value->'ResultRequirements') requirement(value)
                   JOIN jsonb_array_elements(@review_evidence::jsonb) fact(value) ON fact.value->>'key'=requirement.value->>'Key'
                   WHERE step.value->>'Key'=(SELECT workflow_step_key FROM control_plane.run_nodes physical WHERE physical.id=completed.id)
                       AND requirement.value->>'Kind' IN ('REVIEW','QA') AND fact.value->>'verdict'='CHANGES_REQUIRED'
               )) ELSE '{}'::jsonb END AS entry
    FROM completed
    JOIN control_plane.runs root ON root.id=completed.root_run_id AND root.organization_id=completed.organization_id
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    WHERE version.spec->>'ResultEvidenceProfile'='GITHUB_REVIEW_QA_V1'
        AND NOT @accepted::boolean AND @reason::text='CHANGES_REQUIRED'
        AND jsonb_typeof(@review_evidence::jsonb)='array' AND jsonb_array_length(@review_evidence::jsonb) BETWEEN 1 AND 4
)
UPDATE control_plane.run_nodes anchor
SET workflow_attempts=anchor.workflow_attempts || jsonb_build_array(outcome.entry),version=anchor.version+1
FROM outcome
WHERE anchor.id=outcome.origin_id AND anchor.organization_id=outcome.organization_id
    AND anchor.workflow_step_origin_node_id IS NULL AND anchor.workflow_business_attempt=1
    AND jsonb_array_length(anchor.workflow_attempts)=outcome.workflow_business_attempt-1
RETURNING (outcome.entry-'reviewEvidence') || jsonb_build_object('evidence',outcome.entry->'reviewEvidence');
