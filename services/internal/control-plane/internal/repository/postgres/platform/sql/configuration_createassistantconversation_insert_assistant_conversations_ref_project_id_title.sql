-- name: configuration_createassistantconversation_insert_assistant_conversations_ref_project_id_title :one
INSERT INTO control_plane.assistant_conversations(
    ref,organization_id,project_id,session_id,title,state,created_by,
    title_source,context_route,context_entity_kind,context_entity_ref,
    context_entity_name,context_entity_version,allowed_operations,
    assistant_scope,assistant_agent_id,assistant_profile_id
) VALUES(
    $1,$2::uuid,$3::uuid,$4::uuid,'i18n:NEW_ASSISTANT_CONVERSATION','ACTIVE',$5::uuid,
    'SERVER_DEFAULT',$6,$7,$8,$9,$10,$11,$12,$13::uuid,
    (SELECT id FROM control_plane.project_assistant_profiles WHERE organization_id=$2::uuid AND ref=$14)
)
RETURNING ref,title,title_source,title_revision,state,version,created_at,updated_at
