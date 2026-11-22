UPDATE control_plane.integration_personal_credential_tests
SET credential_kind='TOKEN',broker_identity_revision_id=NULL,broker_purpose=NULL
WHERE ref=$1;
