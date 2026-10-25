-- name: repository_bundle__resolve :one
SELECT publication.ref,gate.ref
FROM control_plane.repository_bundle_publications publication
JOIN control_plane.owner_gates gate ON gate.id=publication.owner_gate_id AND gate.organization_id=publication.organization_id
WHERE publication.organization_id=@organization_id::uuid
  AND ((@proposal_ref<>'' AND @gate_ref='' AND publication.ref=@proposal_ref)
    OR (@proposal_ref='' AND @gate_ref<>'' AND gate.ref=@gate_ref));
