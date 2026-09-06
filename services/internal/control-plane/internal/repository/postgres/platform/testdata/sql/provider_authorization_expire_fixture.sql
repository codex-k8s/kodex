UPDATE control_plane.provider_authorization_attempts
SET reservation_deadline=clock_timestamp()-interval '1 second'
WHERE ref=$1 AND preparation_state='RESERVED';
