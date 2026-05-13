# vim: syntax=dockerfile
FROM node:14.17 AS web-build
WORKDIR /app

COPY .ca-bundle /usr/local/share/ca-certificates/
RUN find /usr/local/share/ca-certificates/ -type f -exec cat {} \; >> /etc/ssl/certs/ca-certificates.crt
ENV NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt

COPY web/package*.json .
RUN --mount=type=cache,target=/app/npm/cache \
    npm ci --cache /app/npm/cache

ADD web/ .
RUN npm run build


FROM ghcr.io/astral-sh/uv:python3.13-alpine AS server-build

ARG VERSION
ARG RELEASE
ARG WHEEL_PATH
ARG WEB_PATH
ARG VCS_REF
ARG BUILD_DATE=now

COPY .ca-bundle /usr/local/share/ca-certificates/
RUN find /usr/local/share/ca-certificates/ -type f -exec cat {} \; >> /etc/ssl/certs/ca-certificates.crt

ENV CA_REQUESTS_BUNDLE /etc/ssl/certs/ca-certificates.crt
ENV UV_NATIVE_TLS=true
ENV UV_LINK_MODE=copy
ENV UV_CACHE_DIR=/opt/uv-cache

RUN apk add --no-cache \
      gcc==13.2.1_git20240309-r1 \
      libldap==2.6.8-r0 \
      musl-dev==1.2.5-r1 \
      libffi-dev==3.4.6-r0

LABEL org.label-schema.build-date=$BUILD_DATE \
      org.label-schema.url="https://snoozeweb.net" \
      org.label-schema.vcs-url="https://github.com/snoozeweb/snooze" \
      org.label-schema.vcs-ref=$RELEASE \
      org.label-schema.version=${VERSION}-${RELEASE} \
      org.label-schema.schema-version="1.0.0-rc.1"

WORKDIR /app
COPY pyproject.toml uv.lock README.md /app/

RUN --mount=type=cache,target=/opt/uv-cache \
    --mount=type=bind,source=uv.lock,target=uv.lock \
    --mount=type=bind,source=pyproject.toml,target=pyproject.toml \
    --mount=type=bind,source=README.md,target=README.md \
    uv sync --locked --no-install-project --compile-bytecode --no-editable

COPY src/ ./
RUN --mount=type=cache,target=/opt/uv-cache \
    uv sync --locked --compile-bytecode --no-editable

# Copy and install the web
RUN mkdir -p /app/web
COPY --from=web-build /app/dist/ /app/web/


FROM python:3.13-alpine

USER 1001
WORKDIR /app

COPY --from=server-build --chown=1001:1001 /app/.venv /app/.venv
COPY --from=web-build --chown=1001:1001 /app/dist/ /app/web/

ENV PATH="/app/.venv/bin:$PATH"

CMD ["snooze-server"]
