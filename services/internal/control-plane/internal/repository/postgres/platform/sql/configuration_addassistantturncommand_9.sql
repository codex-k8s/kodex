-- name: platform__configuration_addassistantturncommand_9 :one
INSERT INTO control_plane.assistant_plans(ref,organization_id,conversation_ref,summary,operations,state) VALUES($1,$2::uuid,$3,$4,$5,'PROPOSED') RETURNING id::text
