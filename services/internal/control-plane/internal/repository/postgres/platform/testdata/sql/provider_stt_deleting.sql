UPDATE control_plane.provider_accounts SET state='DELETING',enabled=false WHERE ref=$1;
