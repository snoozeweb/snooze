# vim: syntax=dockerfile
FROM python:3.6-alpine

#ENV PYTHONUNBUFFERED 1
#ENV PIP_DISABLE_PIP_VERSION_CHECK=1
#ENV PIP_NO_CACHE_DIR=1

ARG VERSION
ARG RELEASE
ARG WHEEL_PATH
ARG WEB_PATH
ARG VCS_REF
ARG BUILD_DATE=now

RUN update-ca-certificates
ENV CA_REQUESTS_BUNDLE /etc/ssl/certs/ca-certificates.crt
ENV PIP_CERT /etc/ssl/certs/ca-certificates.crt

LABEL org.label-schema.build-date=$BUILD_DATE \
      org.label-schema.url="https://snoozeweb.net" \
      org.label-schema.vcs-url="https://github.com/snoozeweb/snooze" \
      org.label-schema.vcs-ref=$RELEASE \
      org.label-schema.version=${VERSION}-${RELEASE} \
      org.label-schema.schema-version="1.0.0-rc.1"

# SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# hadolint ignore=DL3008
#RUN sed -i 's/https/http/g' /etc/apk/repositories
RUN apk update && \
    apk --no-cache add \
    curl \
    git \
    gcc \
    musl-dev \
    linux-headers \
    libldap \
    python3-dev \
    libffi-dev

RUN pip install --no-cache-dir poetry==1.2.0a2 pip==21.0.1 virtualenv==20.4.4
RUN python3 -m venv /opt/snooze
RUN mkdir -p /opt/snooze/web
COPY dist /dist
RUN find /dist
RUN /opt/snooze/bin/pip install --no-cache-dir "/${WHEEL_PATH}"
RUN tar -xvf "/${WEB_PATH}" -C /opt/snooze/web

# hadolint ignore=DL3013
ENV PATH $PATH:/venv/bin

RUN mkdir -p /opt/snooze /var/lib/snooze /etc/snooze/server /var/run/snooze /var/log/snooze
RUN chgrp -R 0 /opt/snooze /var/lib/snooze /etc/snooze/server /var/run/snooze /var/log/snooze && \
    chmod -R g=u /opt/snooze /var/lib/snooze /etc/snooze/server /var/run/snooze /var/log/snooze && \
    adduser -D -u 1001 -G root -h /var/lib/snooze snooze

USER 1001

COPY docker-entrypoint.sh /usr/local/bin/

WORKDIR /var/lib/snooze

ENTRYPOINT ["/opt/snooze/bin/snooze-server"]

EXPOSE 5200
