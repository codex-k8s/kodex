UPDATE control_plane.agents SET state=$2,enabled=$3,version=version+1 WHERE ref=$1;
