CREATE SCHEMA auth;

CREATE TABLE auth.users (
 id text PRIMARY KEY,
 username text NOT NULL UNIQUE CHECK (username ~ '^[a-z][a-z0-9._-]{2,59}$'),
 password_hash text NOT NULL,
 role text NOT NULL CHECK (role IN ('Viewer','Operator','Engineer','Incident Commander','Administrator')),
 active boolean NOT NULL DEFAULT true,
 failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts BETWEEN 0 AND 5),
 locked_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE auth.sessions (
 token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash)=32),
 user_id text NOT NULL REFERENCES auth.users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 CHECK (expires_at > created_at)
);
CREATE INDEX auth_sessions_user ON auth.sessions(user_id,expires_at DESC);

CREATE TABLE auth.events (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id text REFERENCES auth.users(id),
 username_digest bytea NOT NULL CHECK (octet_length(username_digest)=32),
 action text NOT NULL CHECK (action IN ('bootstrap_admin','login_succeeded','login_failed','login_locked','logout')),
 at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION auth.prevent_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'authentication events are append-only';
END;
$$;
CREATE TRIGGER immutable_auth_events BEFORE UPDATE OR DELETE ON auth.events
 FOR EACH ROW EXECUTE FUNCTION auth.prevent_event_mutation();
