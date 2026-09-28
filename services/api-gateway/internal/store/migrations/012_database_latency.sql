ALTER TABLE simulation.runs ADD COLUMN scenario text NOT NULL DEFAULT 'payment-decline';
ALTER TABLE simulation.runs ADD COLUMN delay_ms integer NOT NULL DEFAULT 0;
ALTER TABLE simulation.runs ADD CONSTRAINT simulation_scenario CHECK (
 (scenario='payment-decline' AND delay_ms=0) OR
 (scenario='database-latency' AND delay_ms BETWEEN 100 AND 4500)
);
