-- name: platform__queries_getagent_1 :one
SELECT a.ref,COALESCE(p.ref,''),COALESCE(a.system_key,''),a.name,a.purpose,a.role_description,a.avatar_url,a.state,a.enabled,a.version,
		a.runtime_key,r.name,r.provider,r.model,r.runtime_revision,a.capabilities,a.knowledge_artifact_refs,a.created_at,a.updated_at
		FROM control_plane.agents a LEFT JOIN control_plane.projects p ON p.id=a.project_id JOIN control_plane.runtime_profiles r ON r.stable_key=a.runtime_key
		WHERE a.organization_id=$1::uuid AND a.ref=$2 AND (a.system_key='system-assistant' OR $3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=a.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
