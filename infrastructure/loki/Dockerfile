FROM grafana/loki:3.7.8
COPY loki.yml /etc/loki/config.yml
COPY rules /etc/loki/rules
USER 10001
CMD ["-config.file=/etc/loki/config.yml"]
