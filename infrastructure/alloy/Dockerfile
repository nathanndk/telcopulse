FROM grafana/alloy:v1.20.1
# Runs as root: discovering and tailing containers needs the Docker socket, which
# is root-equivalent anyway (see .trivyignore.yaml and docs/observability.md#logs).
COPY config.alloy /etc/alloy/config.alloy
CMD ["run", "--server.http.listen-addr=0.0.0.0:12345", "--storage.path=/var/lib/alloy/data", "--disable-reporting", "/etc/alloy/config.alloy"]
