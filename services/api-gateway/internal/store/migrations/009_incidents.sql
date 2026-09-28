CREATE SCHEMA IF NOT EXISTS incident;
CREATE TABLE incident.records (
 id text PRIMARY KEY,
 creation_key text NOT NULL UNIQUE,
 request_hash text NOT NULL,
 environment text NOT NULL CHECK(environment IN ('development','staging')),
 state text NOT NULL CHECK(state IN ('Detected','Acknowledged','Investigating','Identified','Mitigating','Monitoring','Resolved','Postmortem')),
 version bigint NOT NULL CHECK(version > 0),
 document jsonb NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(document->>'id'=id),
 CHECK(document->>'state'=state),
 CHECK(document->>'environment'=environment),
 CHECK((document->>'version')::bigint=version)
);
CREATE INDEX incident_filter ON incident.records(environment,state,updated_at DESC,id);
CREATE TABLE incident.audit (
 incident_id text NOT NULL REFERENCES incident.records(id),
 version bigint NOT NULL,
 entry jsonb NOT NULL,
 PRIMARY KEY(incident_id,version)
);
CREATE FUNCTION incident.prevent_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'incident audit entries are append-only';
END;
$$;
CREATE TRIGGER immutable_incident_audit BEFORE UPDATE OR DELETE ON incident.audit
 FOR EACH ROW EXECUTE FUNCTION incident.prevent_audit_mutation();
