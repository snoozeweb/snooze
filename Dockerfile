FROM python:3.7-alpine

ENV PYTHONUNBUFFERED 1
ENV PIP_DISABLE_PIP_VERSION_CHECK=1
ENV PIP_NO_CACHE_DIR=1

ARG BUILD_DATE=now
ARG VCS_REF
ARG VERSION

ENV APP_VERSION=1.0.8

LABEL org.label-schema.build-date=$BUILD_DATE \
      org.label-schema.url="https://snoozeweb.net" \
      org.label-schema.vcs-url="https://github.com/snoozeweb/snooze" \
      org.label-schema.vcs-ref=$VCS_REF \
      org.label-schema.version=$VERSION \
      org.label-schema.schema-version="1.0.0-rc.1"

# SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# hadolint ignore=DL3008
RUN sed -i 's/https/http/g' /etc/apk/repositories
RUN apk update && \
    apk --no-cache add \
    curl \
    git \
    gcc \
    musl-dev \
    linux-headers \
    libldap \
    python3-dev

COPY requirements.txt /app/
# hadolint ignore=DL3013
RUN pip install --no-cache-dir pip virtualenv && \
    python3 -m venv /venv && \
    /venv/bin/pip install --no-cache-dir --upgrade setuptools && \
    /venv/bin/pip install --no-cache-dir --requirement /app/requirements.txt
ENV PATH $PATH:/venv/bin

RUN /venv/bin/pip install snooze-server==${APP_VERSION}
ADD https://github.com/snoozeweb/snooze/releases/download/v${APP_VERSION}/snooze-web-${APP_VERSION}.tar.gz /tmp/snooze-web.tar.gz
RUN tar axvf /tmp/snooze-web.tar.gz -C /

RUN mkdir -p /opt/snooze /var/lib/snooze /etc/snooze/server
RUN chgrp -R 0 /venv /opt/snooze /var/lib/snooze /etc/snooze/server && \
    chmod -R g=u /venv /opt/snooze /var/lib/snooze /etc/snooze/server && \
    adduser -D -u 1001 -G root -h /var/lib/snooze snooze

USER 1001

COPY docker-entrypoint.sh /usr/local/bin/

WORKDIR /var/lib/snooze

ENTRYPOINT ["sh", "/usr/local/bin/docker-entrypoint.sh"]

EXPOSE 5200
