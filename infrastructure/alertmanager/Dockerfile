FROM prom/alertmanager:v0.34.1
COPY alertmanager.yml telcopulse.tmpl entrypoint.sh /etc/alertmanager/
USER nobody
ENTRYPOINT ["/bin/sh", "/etc/alertmanager/entrypoint.sh"]
CMD ["--storage.path=/alertmanager"]
