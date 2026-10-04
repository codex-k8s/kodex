-- name: configuration_auditassistantoperation_insert_audit_events_ref_project_id_assistant_agent_id :exec
INSERT INTO control_plane.audit_events(ref,organization_id,project_id,actor_id,assistant_agent_id,action,resource_kind,resource_ref,outcome,safe_summary,correlation_ref)
SELECT $1,$2::uuid,$3::uuid,$4::uuid,conversation.assistant_agent_id,$5,$6,$7,'SUCCEEDED',$8,$9
FROM control_plane.assistant_conversations conversation
WHERE conversation.organization_id=$2::uuid AND conversation.created_by=$4::uuid AND conversation.ref=$10;
