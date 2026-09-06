-- name: prompt_impact_consumers :many
SELECT COALESCE(p.ref,''),b.consumer_kind,b.consumer_ref,
 COALESCE(a.version,w.version,s.version),b.ref,b.version,r.ref
FROM control_plane.managed_configuration_bindings b
JOIN control_plane.managed_configuration_revisions r ON r.id=b.configuration_revision_id AND r.configuration_set_id=b.configuration_set_id
LEFT JOIN control_plane.agents a ON b.consumer_kind IN ('AGENT','AGENT_CONTINUATION') AND a.ref=b.consumer_ref AND a.organization_id=b.organization_id AND (a.project_id IS NOT DISTINCT FROM b.project_id OR b.project_id IS NULL AND b.consumer_kind='AGENT') AND a.state<>'ARCHIVED'
LEFT JOIN control_plane.workflows w ON b.consumer_kind='WORKFLOW' AND w.ref=b.consumer_ref AND w.organization_id=b.organization_id AND (b.project_id IS NULL OR w.project_id=b.project_id) AND w.state<>'ARCHIVED'
LEFT JOIN control_plane.schedules s ON b.consumer_kind='SCHEDULE' AND s.ref=b.consumer_ref AND s.organization_id=b.organization_id AND (b.project_id IS NULL OR s.project_id=b.project_id) AND s.lifecycle_state<>'ARCHIVED'
LEFT JOIN control_plane.projects p ON p.id=COALESCE(a.project_id,w.project_id,s.project_id,b.project_id) AND p.organization_id=b.organization_id
WHERE b.organization_id=@organization_id::uuid AND b.configuration_set_id=@configuration_id::uuid
 AND b.configuration_kind='PROMPT_TEMPLATE' AND COALESCE(a.version,w.version,s.version) IS NOT NULL
ORDER BY b.ref LIMIT 1001;
