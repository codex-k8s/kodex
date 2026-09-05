-- name: prompt_continuation_insert :exec
INSERT INTO control_plane.session_continuation_notices
    (organization_id, session_id, turn_id, node_id, attempt, previous_runtime_revision_id,
     current_runtime_revision_id, template_ref, template_digest, service_template_revision,
     service_template_digest, variable_snapshot_digest, diff_digest, materialization_digest,
     content, safe_snapshot)
SELECT current.organization_id, current.session_id, current.turn_id, current.node_id, current.attempt,
       previous.id, current.id, @template_ref, @template_digest, @service_revision,
       @service_digest, @variable_digest, @diff_digest, @materialization_digest, @content, @snapshot::jsonb
FROM control_plane.runtime_revisions current
JOIN control_plane.runtime_revisions previous ON previous.id = @previous_id::uuid
 AND previous.organization_id = current.organization_id AND previous.session_id = current.session_id
WHERE current.id = @current_id::uuid AND current.organization_id = @organization_id::uuid;
