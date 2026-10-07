#!/bin/sh
set -eu
mkdir -p "${BACKUP_DIR:-/backups}" "${TEXTFILE_DIR:-/textfile}"
chown -R postgres:postgres "${BACKUP_DIR:-/backups}" "${TEXTFILE_DIR:-/textfile}"
# The Compose secret is root-readable only; hand the job a private copy.
install -m 0400 -o postgres -g postgres /run/secrets/postgres_password /tmp/postgres_password
exec su-exec postgres env POSTGRES_PASSWORD_FILE=/tmp/postgres_password /bin/sh /usr/local/bin/telcopulse-backup
