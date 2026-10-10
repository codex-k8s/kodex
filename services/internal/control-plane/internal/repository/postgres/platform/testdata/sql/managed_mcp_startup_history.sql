SELECT md5(to_jsonb(receipt)::text)
FROM control_plane.integration_connection_tests receipt
WHERE ref=@test_ref;
