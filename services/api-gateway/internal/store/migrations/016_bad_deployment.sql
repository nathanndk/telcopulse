ALTER TABLE simulation.runs ADD COLUMN deployment_id text;
ALTER TABLE simulation.runs DROP CONSTRAINT simulation_scenario;
ALTER TABLE simulation.runs ADD CONSTRAINT simulation_scenario CHECK (
 (scenario='payment-decline' AND delay_ms=0 AND deployment_id IS NULL) OR
 (scenario IN ('database-latency','kafka-consumer-lag','database-timeout') AND delay_ms BETWEEN 100 AND 4500 AND deployment_id IS NULL) OR
 (scenario='bad-deployment' AND delay_ms=0 AND deployment_id ~ '^sim-deploy-[a-f0-9]{24}$')
);
CREATE TABLE simulation.deployment_outbox (
 run_id text NOT NULL REFERENCES simulation.runs(id),
 status text NOT NULL CHECK(status IN ('Completed','Rolled Back')),
 event_id text NOT NULL UNIQUE,
 occurred_at timestamptz NOT NULL,
 delivered_at timestamptz,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(run_id,status)
);
CREATE INDEX simulation_deployment_pending ON simulation.deployment_outbox(next_attempt_at,occurred_at)
 WHERE delivered_at IS NULL;
