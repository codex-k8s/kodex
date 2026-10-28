-- name: ssh_compose__store_test_snapshot :exec
UPDATE control_plane.integration_connection_tests
SET ssh_compose_snapshot=@snapshot::jsonb
WHERE id=@test_id::uuid AND ssh_compose_snapshot IS NULL;
