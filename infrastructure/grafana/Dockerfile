FROM grafana/grafana:13.2.2
COPY provisioning /etc/grafana/provisioning
COPY dashboards /etc/grafana/dashboards
# Launch the verified server binary directly: this pinned ARM64 image ships an
# empty /run.sh. Keep paths explicit so named-volume data remains persistent.
ENTRYPOINT ["/usr/share/grafana/bin/grafana", "server", "--homepath=/usr/share/grafana", "--config=/etc/grafana/grafana.ini", "cfg:default.paths.data=/var/lib/grafana", "cfg:default.paths.logs=/var/log/grafana", "cfg:default.paths.plugins=/var/lib/grafana/plugins", "cfg:default.paths.provisioning=/etc/grafana/provisioning"]
