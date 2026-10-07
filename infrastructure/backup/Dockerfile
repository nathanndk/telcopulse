FROM postgres:17-alpine
COPY backup.sh /usr/local/bin/telcopulse-backup
ENTRYPOINT ["/bin/sh", "/usr/local/bin/telcopulse-backup"]
