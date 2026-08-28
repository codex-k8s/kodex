-- name: configuration_addassistantturncommand_insert_assistant_plans_ref_conversation_ref_operations :one
INSERT INTO control_plane.assistant_plans(ref,organization_id,conversation_ref,summary,operations,state,current_revision,content_digest)
VALUES($1,$2::uuid,$3,$4,$5,'DRAFT',1,$6)
RETURNING id::text
