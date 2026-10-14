-- name: assistant_prepared_content_source :one
SELECT conversation.assistant_scope,
       CASE WHEN conversation.assistant_scope='PROJECT' THEN profile.ref ELSE agent.ref END,
       CASE WHEN conversation.assistant_scope='PROJECT' THEN profile.version ELSE agent.version END,
       encode(sha256(convert_to((revision.safe_snapshot->'assistantContext')::text,'UTF8')),'hex')
FROM control_plane.runtime_revisions revision
JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id AND lease.organization_id=revision.organization_id
JOIN control_plane.agents agent ON agent.id=revision.agent_id AND agent.organization_id=revision.organization_id
JOIN control_plane.assistant_conversations conversation ON conversation.session_id=revision.session_id AND conversation.organization_id=revision.organization_id
 AND conversation.assistant_agent_id=agent.id AND conversation.created_by=$4::uuid AND conversation.state='ACTIVE'
LEFT JOIN control_plane.project_assistant_profiles profile ON profile.id=conversation.assistant_profile_id
 AND profile.organization_id=revision.organization_id AND profile.project_id=conversation.project_id AND profile.agent_id=agent.id
WHERE revision.organization_id=$1::uuid AND revision.id=$2::uuid AND lease.id=$3::uuid
 AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp() AND lease.generation=revision.generation
 AND control_plane.runtime_execution_before_deadline(lease.organization_id,lease.run_id)
 AND (NOT $5::boolean OR ('platform.artifact.manage'=ANY(agent.capabilities) AND revision.safe_snapshot->'capabilities' ? 'platform.artifact.manage'))
 AND revision.safe_snapshot->>'assistantScope'=conversation.assistant_scope
 AND ((conversation.assistant_scope='PROJECT' AND profile.id IS NOT NULL AND profile.ref=revision.safe_snapshot->>'assistantProfileRef'
       AND revision.project_id=conversation.project_id)
   OR (conversation.assistant_scope='SYSTEM' AND agent.system_key='system-assistant' AND agent.project_id IS NULL))
 AND jsonb_typeof(revision.safe_snapshot->'assistantContext')='object'
