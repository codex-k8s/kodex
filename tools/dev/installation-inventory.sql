-- Read-only счётчики перед полной очисткой #2108; без task, transcript, identities и credentials.
BEGIN READ ONLY;
SELECT json_build_object(
    'schemaVersion', (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
    'sessions', (SELECT count(*) FROM control_plane.sessions),
    'runsByState', (SELECT json_object_agg(state, total) FROM (
        SELECT state, count(*) AS total FROM control_plane.runs GROUP BY state
    ) AS counts),
    'leasesByState', (SELECT json_object_agg(state, total) FROM (
        SELECT state, count(*) AS total FROM control_plane.runtime_leases GROUP BY state
    ) AS counts)
);
ROLLBACK;
