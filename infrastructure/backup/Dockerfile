FROM postgres:17-alpine
RUN apk add --no-cache su-exec
COPY backup.sh /usr/local/bin/telcopulse-backup
COPY entrypoint.sh /usr/local/bin/telcopulse-backup-entrypoint
# Starts as root only to give the postgres user the backup and metrics volumes,
# which Docker creates root-owned, then drops privileges (see .trivyignore.yaml).
ENTRYPOINT ["/bin/sh", "/usr/local/bin/telcopulse-backup-entrypoint"]
