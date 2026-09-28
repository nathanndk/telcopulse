CREATE TABLE incident.saved_views (
 id text PRIMARY KEY,
 owner text NOT NULL,
 environment text NOT NULL CHECK (environment IN ('development','staging')),
 name text NOT NULL,
 filters jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX incident_saved_views_owner_name ON incident.saved_views(owner,environment,lower(name));
CREATE INDEX incident_saved_views_owner_recent ON incident.saved_views(owner,environment,updated_at DESC,id);
