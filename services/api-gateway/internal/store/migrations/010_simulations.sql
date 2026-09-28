CREATE SCHEMA simulation;
CREATE TABLE simulation.runs (
 id text PRIMARY KEY,
 creation_key text NOT NULL UNIQUE,
 request jsonb NOT NULL,
 environment text NOT NULL CHECK(environment IN ('development','staging')),
 percentage integer NOT NULL CHECK(percentage BETWEEN 1 AND 100),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 stopped_at timestamptz,
 reason text NOT NULL,
 CHECK(expires_at > started_at)
);
CREATE INDEX simulation_active ON simulation.runs(environment,expires_at);
CREATE TABLE simulation.decisions (
 transaction_id text PRIMARY KEY,
 environment text NOT NULL CHECK(environment IN ('development','staging')),
 run_id text REFERENCES simulation.runs(id),
 inject boolean NOT NULL,
 at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE simulation.audit (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 run_id text NOT NULL REFERENCES simulation.runs(id),
 action text NOT NULL CHECK(action IN ('started','stopped')),
 actor text NOT NULL,
 note text NOT NULL,
 at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER immutable_simulation_audit BEFORE UPDATE OR DELETE ON simulation.audit
 FOR EACH ROW EXECUTE FUNCTION incident.prevent_audit_mutation();
