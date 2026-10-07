FROM cr.jaegertracing.io/jaegertracing/jaeger:2.21.0
COPY config.yml /etc/jaeger/config.yml
USER 10001
CMD ["--config=/etc/jaeger/config.yml"]
