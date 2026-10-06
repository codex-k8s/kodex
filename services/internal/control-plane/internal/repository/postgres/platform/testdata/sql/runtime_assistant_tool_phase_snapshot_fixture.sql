UPDATE control_plane.runtime_revisions
SET safe_snapshot=jsonb_set(safe_snapshot,ARRAY[$2::text],to_jsonb($3::text))
WHERE ref=$1;
