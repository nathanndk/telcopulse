CREATE INDEX transactions_failed_incident_window
 ON transactions(environment,created_at DESC,id DESC)
 WHERE status='FAILED';
