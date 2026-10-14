SELECT revision.safe_snapshot::text,catalog.digest,
 (SELECT jsonb_agg(to_jsonb(entry) ORDER BY entry.ref)::text FROM control_plane.runtime_file_catalog_entries entry WHERE entry.catalog_id=catalog.id)
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
JOIN control_plane.runtime_file_catalogs catalog ON catalog.runtime_revision_ref=revision.ref
WHERE lease.ref=$1;
