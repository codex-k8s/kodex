-- name: platform__configuration_changeconnection_insert_integration_connection_tests_ref_connection_id_created_by :exec
INSERT INTO control_plane.integration_connection_tests(ref,organization_id,connection_id,state,created_by) VALUES($1,$2::uuid,$3::uuid,'DUE',$4::uuid)
