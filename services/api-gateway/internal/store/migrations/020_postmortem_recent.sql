CREATE INDEX postmortems_recent ON incident.postmortems(generated_at DESC,incident_id DESC);
