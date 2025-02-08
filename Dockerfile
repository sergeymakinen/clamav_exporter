FROM quay.io/prometheus/busybox:latest
LABEL maintainer="Sergey Makinen <sergey@makinen.ru>"

ARG TARGETOS
ARG TARGETARCH
COPY dist/clamav_exporter_${TARGETOS}_${TARGETARCH}/clamav_exporter /bin/clamav_exporter

EXPOSE 9906
USER nobody
ENTRYPOINT ["/bin/clamav_exporter"]
