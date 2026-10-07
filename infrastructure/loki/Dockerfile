FROM grafana/loki:3.7.8
COPY loki.yml /etc/loki/config.yml
COPY rules /etc/loki/rules
CMD ["-config.file=/etc/loki/config.yml"]
