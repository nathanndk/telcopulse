FROM grafana/alloy:v1.20.1
COPY config.alloy /etc/alloy/config.alloy
CMD ["run", "--server.http.listen-addr=0.0.0.0:12345", "--storage.path=/var/lib/alloy/data", "--disable-reporting", "/etc/alloy/config.alloy"]
