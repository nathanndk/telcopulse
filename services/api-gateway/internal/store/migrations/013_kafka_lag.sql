ALTER TABLE simulation.runs DROP CONSTRAINT simulation_scenario;
ALTER TABLE simulation.runs ADD CONSTRAINT simulation_scenario CHECK (
 (scenario='payment-decline' AND delay_ms=0) OR
 (scenario IN ('database-latency','kafka-consumer-lag') AND delay_ms BETWEEN 100 AND 4500)
);
