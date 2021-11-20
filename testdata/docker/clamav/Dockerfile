FROM debian:bullseye

RUN set -x \
    && apt-get update \
    && apt-get install -y clamav-daemon \
    && rm -rf /var/lib/apt/lists/* \
    && set +x

COPY rootfs /

RUN echo 'TCPAddr 0.0.0.0' >> /etc/clamav/clamd.conf \
    && echo 'TCPSocket 3310' >> /etc/clamav/clamd.conf \
    && sed -i 's/^Foreground .*/Foreground true/' /etc/clamav/clamd.conf \
    && mkdir -p /var/run/clamav \
    && chown -R clamav:clamav /var/lib/clamav /var/run/clamav

CMD /usr/sbin/clamd
