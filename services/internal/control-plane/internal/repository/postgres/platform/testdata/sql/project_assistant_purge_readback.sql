SELECT (SELECT count(*) FROM control_plane.project_assistant_profiles WHERE ref=$1),
       (SELECT count(*) FROM control_plane.assistant_conversations WHERE ref=$2),
       (SELECT count(*) FROM control_plane.sessions WHERE ref=$3),
       (SELECT count(*) FROM control_plane.project_assistant_profiles WHERE ref=$4),
       (SELECT count(*) FROM control_plane.assistant_conversations WHERE ref=$5);
