ARG NODE_IMAGE=node:22.13.1-alpine
ARG NGINX_IMAGE=nginxinc/nginx-unprivileged:1.27-alpine

FROM ${NODE_IMAGE} AS build

WORKDIR /workspace/services/staff/web-console

COPY services/staff/web-console/package.json services/staff/web-console/package-lock.json ./
RUN npm ci

COPY specs/openapi/staff-gateway.v1.yaml /workspace/specs/openapi/staff-gateway.v1.yaml
COPY services/staff/web-console/ ./

RUN npm run build

FROM ${NODE_IMAGE} AS dev

WORKDIR /workspace/services/staff/web-console

COPY --chown=node:node services/staff/web-console/package.json services/staff/web-console/package-lock.json ./
RUN npm ci && chown -R node:node /workspace/services/staff/web-console

COPY --chown=node:node specs/openapi/staff-gateway.v1.yaml /workspace/specs/openapi/staff-gateway.v1.yaml
COPY --chown=node:node services/staff/web-console/ ./

USER node

EXPOSE 5174

CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]

FROM ${NGINX_IMAGE} AS prod

COPY --from=build /workspace/services/staff/web-console/dist/ /usr/share/nginx/html/
COPY services/staff/web-console/nginx/default.conf /etc/nginx/conf.d/default.conf

EXPOSE 8080

USER 101:101
