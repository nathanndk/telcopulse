ALTER TABLE auth.events ADD COLUMN actor_user_id text REFERENCES auth.users(id);
ALTER TABLE auth.events ADD COLUMN details jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details) = 'object');
ALTER TABLE auth.events DROP CONSTRAINT events_action_check;
ALTER TABLE auth.events ADD CONSTRAINT events_action_check CHECK (action IN (
 'bootstrap_admin','login_succeeded','login_failed','login_locked','logout',
 'user_created','role_changed','user_deactivated','user_reactivated'
));
CREATE INDEX auth_users_created ON auth.users(created_at,id);
