-- name: target_turn_attempts__list :many
SELECT jsonb_build_object(
    'id', attempt.turn_id::text || '#' || attempt.attempt::text,
    'organization_id', turn.organization_id,
    'project_id', turn.project_id,
    'owner_actor_id', turn.owner_actor_id,
    'kind', 'TURN_ATTEMPT',
    'state', attempt.state,
    'version', attempt.lease_fence,
    'spec', jsonb_build_object(
        'turnId', attempt.turn_id,
        'attempt', attempt.attempt,
        'workloadId', attempt.workload_id,
        'authorityGeneration', attempt.authority_generation,
        'inputSha256', attempt.input_sha256,
        'runtimeRevisionId', attempt.runtime_revision_id,
        'runtimeRevisionVersion', attempt.runtime_revision_version
    )
)::text
FROM control_plane.turn_attempts AS attempt
JOIN control_plane.resources AS turn ON turn.id = attempt.turn_id AND turn.kind = 'TURN'
ORDER BY turn.organization_id, turn.project_id, attempt.turn_id, attempt.attempt;
