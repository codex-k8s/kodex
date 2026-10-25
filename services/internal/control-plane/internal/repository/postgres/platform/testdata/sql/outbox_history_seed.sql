INSERT INTO control_plane.outbox_events
    (event_id, subject, ordering_key, sequence, payload, state, published_at, broker_receipt)
SELECT gen_random_uuid(), 'fixture.events', 'fixture.history', n,
       convert_to(n::text, 'UTF8'),
       CASE WHEN n <= 70000 THEN 'PUBLISHED' ELSE 'PENDING' END,
       CASE WHEN n <= 70000 THEN clock_timestamp() ELSE NULL END,
       CASE WHEN n <= 70000 THEN 'fixture:1:false' ELSE NULL END
FROM generate_series(1, 78000) n;

INSERT INTO control_plane.outbox_events
    (event_id, subject, ordering_key, sequence, payload, state, available_at, lease_expires_at)
VALUES
    (gen_random_uuid(), 'fixture.events', 'fixture.dead', 1, 'dead', 'DEAD_LETTER', clock_timestamp(), NULL),
    (gen_random_uuid(), 'fixture.events', 'fixture.dead', 2, 'blocked-dead', 'PENDING', clock_timestamp(), NULL),
    (gen_random_uuid(), 'fixture.events', 'fixture.leased', 1, 'leased', 'CLAIMED', clock_timestamp(), clock_timestamp()+interval '1 hour'),
    (gen_random_uuid(), 'fixture.events', 'fixture.leased', 2, 'blocked-lease', 'PENDING', clock_timestamp(), NULL),
    (gen_random_uuid(), 'fixture.events', 'fixture.future', 1, 'future', 'PENDING', clock_timestamp()+interval '1 hour', NULL),
    (gen_random_uuid(), 'fixture.events', 'fixture.future', 2, 'blocked-future', 'PENDING', clock_timestamp(), NULL),
    (gen_random_uuid(), 'fixture.events', 'fixture.independent', 1, 'independent', 'PENDING', clock_timestamp(), NULL);

ANALYZE control_plane.outbox_events;
