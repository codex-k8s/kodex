-- name: prompt_preview_session_sets :many
SELECT attachment.id::text, attachment.ref, attachment.manifest_digest,
       attachment.purpose, attachment.item_count, attachment.total_size_bytes
FROM (
    SELECT turn.attachment_set_id, min(turn.turn_number) AS first_turn
    FROM control_plane.session_turns turn
    WHERE turn.organization_id=@organization_id::uuid AND turn.session_id=@session_id::uuid
      AND turn.attachment_set_id IS NOT NULL
    GROUP BY turn.attachment_set_id
) selected
JOIN control_plane.attachment_sets attachment ON attachment.id=selected.attachment_set_id
WHERE attachment.organization_id=@organization_id::uuid
  AND attachment.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
  AND attachment.state='FINALIZED' AND attachment.ref<>@current_set_ref
ORDER BY selected.first_turn, attachment.ref
LIMIT 513;
