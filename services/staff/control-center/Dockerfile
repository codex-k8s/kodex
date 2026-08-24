# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/node:24.17.0-alpine3.23@sha256:7c70d1235c0b4c2bc9eeed5393d19f1bbdde6885ba0d58ba62bb385d7b0f3ff1 AS build

WORKDIR /workspace/services/staff/control-center

COPY services/staff/control-center/package.json services/staff/control-center/package-lock.json ./
RUN npm ci --ignore-scripts

COPY services/staff/control-center/index.html services/staff/control-center/eslint.config.js services/staff/control-center/openapi-ts.config.mjs services/staff/control-center/playwright.config.ts services/staff/control-center/playwright.auth.config.ts services/staff/control-center/tsconfig.app.json services/staff/control-center/tsconfig.e2e.json services/staff/control-center/tsconfig.json services/staff/control-center/tsconfig.node.json services/staff/control-center/vite.config.ts ./
COPY services/staff/control-center/e2e/ e2e/
COPY services/staff/control-center/public/ public/
COPY services/staff/control-center/src/ src/

RUN npm run build

FROM docker.io/library/nginx:1.29.4-alpine3.23@sha256:4870c12cd2ca986de501a804b4f506ad3875a0b1874940ba0a2c7f763f1855b2

ARG SOURCE_SHA=unknown
LABEL org.opencontainers.image.source="https://github.com/codex-k8s/matter-codex" \
      org.opencontainers.image.revision="${SOURCE_SHA}" \
      org.opencontainers.image.title="MatterCodex Staff Control Center"

RUN rm -f /etc/nginx/conf.d/default.conf && \
    mkdir -p /var/cache/nginx/client_temp /var/cache/nginx/proxy_temp /var/run/nginx && \
    chown -R 101:101 /var/cache/nginx /var/run/nginx /usr/share/nginx/html

COPY --chmod=0444 services/staff/control-center/nginx.conf /etc/nginx/nginx.conf
COPY --chmod=0555 services/staff/control-center/nginx-entrypoint.sh /usr/local/bin/staff-control-center-entrypoint
COPY --from=build --chown=101:101 /workspace/services/staff/control-center/dist /usr/share/nginx/html

USER 101:101
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/staff-control-center-entrypoint"]
