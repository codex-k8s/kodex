SELECT o.updated_at,
       (SELECT count(*) FROM control_plane.audit_events a
        WHERE a.organization_id = d.organization_id AND a.resource_ref = s.ref)
FROM control_plane.runtime_secret_draft_operations o
JOIN control_plane.runtime_secret_drafts d ON d.id = o.draft_id
JOIN control_plane.runtime_secrets s ON s.id = d.secret_id
WHERE o.ref = $1;
