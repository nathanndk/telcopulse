CREATE INDEX simulation_decision_run ON simulation.decisions(run_id,transaction_id DESC);
CREATE UNIQUE INDEX simulation_audit_action ON simulation.audit(run_id,action);
