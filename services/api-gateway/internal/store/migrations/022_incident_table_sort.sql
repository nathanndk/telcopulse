ALTER TABLE incident.records ADD COLUMN detected_at timestamptz;
UPDATE incident.records SET detected_at=(document->>'detected_at')::timestamptz;
ALTER TABLE incident.records ALTER COLUMN detected_at SET NOT NULL;

CREATE FUNCTION incident.sync_detected_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.detected_at := (NEW.document->>'detected_at')::timestamptz;
 RETURN NEW;
END;
$$;
CREATE TRIGGER incident_record_detected_at BEFORE INSERT OR UPDATE OF document ON incident.records
 FOR EACH ROW EXECUTE FUNCTION incident.sync_detected_at();

CREATE INDEX incident_detected_order ON incident.records(environment,detected_at DESC,id DESC);
CREATE INDEX incident_severity_order ON incident.records(environment,(document->>'severity'),updated_at DESC,id DESC);
