SET ROLE control_plane_owner;
DROP TRIGGER ssh_native_attachment_fault ON control_plane.integration_personal_credentials;
DROP FUNCTION control_plane.ssh_native_attachment_fault();
RESET ROLE;
