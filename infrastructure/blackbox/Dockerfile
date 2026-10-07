FROM prom/blackbox-exporter:v0.29.0
COPY blackbox.yml /etc/blackbox_exporter/config.yml
# HTTP and TCP probes need no privileges (ICMP would).
USER nobody
