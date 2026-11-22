SET ROLE control_plane_owner;
CREATE FUNCTION control_plane.ssh_native_attachment_fault() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $fault$
BEGIN
 IF NEW.broker_identity_revision_id IS NOT NULL AND NEW.broker_identity_revision_id IS DISTINCT FROM OLD.broker_identity_revision_id THEN
  RAISE EXCEPTION USING ERRCODE='58000',MESSAGE='synthetic attachment dependency unavailable';
 END IF;
 RETURN NEW;
END $fault$;
CREATE TRIGGER ssh_native_attachment_fault BEFORE UPDATE ON control_plane.integration_personal_credentials FOR EACH ROW EXECUTE FUNCTION control_plane.ssh_native_attachment_fault();
RESET ROLE;
