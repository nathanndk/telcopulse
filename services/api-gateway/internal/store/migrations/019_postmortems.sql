CREATE TABLE incident.postmortems (
 incident_id text PRIMARY KEY REFERENCES incident.records(id),
 incident_version bigint NOT NULL CHECK(incident_version > 0),
 document jsonb NOT NULL,
 generated_at timestamptz NOT NULL,
 CHECK(document->>'incident_id'=incident_id),
 CHECK((document->>'incident_version')::bigint=incident_version)
);
CREATE TRIGGER immutable_incident_postmortems BEFORE UPDATE OR DELETE ON incident.postmortems
 FOR EACH ROW EXECUTE FUNCTION incident.prevent_audit_mutation();
