UPDATE control_plane.subjects SET active=false
WHERE id=(SELECT root.initiated_by FROM control_plane.runtime_revisions revision
          JOIN control_plane.runs root ON root.id=revision.root_run_id AND root.organization_id=revision.organization_id
          WHERE revision.ref=$1);
