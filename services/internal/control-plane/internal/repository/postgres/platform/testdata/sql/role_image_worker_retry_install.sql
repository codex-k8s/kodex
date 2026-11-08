CREATE SEQUENCE control_plane.worker_retry_fault_seq;
CREATE FUNCTION control_plane.worker_retry_fault() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
 IF nextval('control_plane.worker_retry_fault_seq') <= TG_ARGV[0]::int THEN
  RAISE EXCEPTION USING ERRCODE=TG_ARGV[1], MESSAGE='synthetic driver detail must remain private';
 END IF;
 RETURN NEW;
END $fn$;
CREATE TRIGGER worker_retry_fault BEFORE %s ON %s
FOR EACH ROW EXECUTE FUNCTION control_plane.worker_retry_fault('%d','%s');
GRANT USAGE, SELECT ON SEQUENCE control_plane.worker_retry_fault_seq TO %s;
