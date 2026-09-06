-- name: runtime_revision_public_pair :many
WITH current_revision AS (
    SELECT revision.id, revision.organization_id, revision.project_id,
           revision.session_id, revision.created_at, revision.ref
    FROM control_plane.runtime_revisions revision
    JOIN control_plane.runs run ON run.id = revision.run_id
    WHERE revision.organization_id = @organization_id::uuid
      AND run.ref = @run_ref
      AND (@revision_ref = '' OR revision.ref = @revision_ref)
    ORDER BY revision.created_at DESC, revision.ref DESC
    LIMIT 1
), selected AS (
    SELECT id, 0 AS ordinal FROM current_revision
    UNION ALL
    SELECT predecessor.id, 1 AS ordinal
    FROM current_revision current
    CROSS JOIN LATERAL (
        SELECT previous.id
        FROM control_plane.runtime_revisions previous
        WHERE previous.organization_id = current.organization_id
          AND previous.project_id IS NOT DISTINCT FROM current.project_id
          AND previous.session_id = current.session_id
          AND (previous.created_at, previous.ref) < (current.created_at, current.ref)
        ORDER BY previous.created_at DESC, previous.ref DESC
        LIMIT 1
    ) predecessor
)
SELECT revision.ref, revision.generation, run.ref, session.ref,
       COALESCE(turn.ref, ''), revision.attempt, revision.revision_digest, revision.created_at,
       revision.provider, revision.model, revision.runtime_profile_key, revision.runtime_profile_revision,
       COALESCE(revision.runtime_config_ref, ''), COALESCE(revision.runtime_config_version, 0), COALESCE(revision.runtime_config_digest, ''),
       COALESCE(revision.provider_policy_ref, ''), COALESCE(revision.provider_policy_version, 0), COALESCE(revision.provider_policy_digest, ''),
       COALESCE(revision.config_overlay_ref, ''), COALESCE(revision.config_overlay_version, 0), COALESCE(revision.config_overlay_digest, ''),
       COALESCE(revision.runtime_environment_ref, ''), COALESCE(revision.runtime_environment_version, 0), COALESCE(revision.runtime_environment_digest, ''),
       COALESCE(revision.environment_binding_ref, ''), COALESCE(revision.environment_binding_version, 0), COALESCE(revision.environment_binding_digest, ''),
       revision.instruction_ref, revision.instruction_digest, revision.integration_grants_digest,
       revision.image_manifest_digest
FROM selected
JOIN control_plane.runtime_revisions revision ON revision.id = selected.id
JOIN control_plane.runs run ON run.id = revision.run_id
JOIN control_plane.sessions session ON session.id = revision.session_id
LEFT JOIN control_plane.session_turns turn ON turn.id = revision.turn_id
ORDER BY selected.ordinal;
