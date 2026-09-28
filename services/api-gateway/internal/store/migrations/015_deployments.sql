CREATE SCHEMA deployment;
CREATE TABLE deployment.records (
 id text PRIMARY KEY,
 service text NOT NULL,
 environment text NOT NULL CHECK (environment IN ('development','staging','production')),
 version text NOT NULL,
 commit_sha text NOT NULL,
 deployer text NOT NULL,
 status text NOT NULL CHECK (status IN ('Pending','Running','Completed','Failed','Rolled Back')),
 occurred_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX deployment_by_service_time ON deployment.records(environment,service,occurred_at DESC,id DESC);
CREATE TABLE deployment.events (
 event_id text PRIMARY KEY,
 deployment_id text NOT NULL REFERENCES deployment.records(id),
 request_hash text NOT NULL,
 actor text NOT NULL,
 status text NOT NULL CHECK (status IN ('Pending','Running','Completed','Failed','Rolled Back')),
 occurred_at timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX deployment_event_history ON deployment.events(deployment_id,occurred_at,event_id);
CREATE FUNCTION deployment.prevent_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'deployment events are append-only';
END;
$$;
CREATE TRIGGER immutable_deployment_events BEFORE UPDATE OR DELETE ON deployment.events
 FOR EACH ROW EXECUTE FUNCTION deployment.prevent_event_mutation();
