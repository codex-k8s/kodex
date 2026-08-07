ARG NODE_IMAGE=node:24.17.1-alpine3.23
FROM ${NODE_IMAGE} AS build
WORKDIR /workspace
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts
COPY . ./
RUN npm run build

ARG NGINX_IMAGE=nginx:1.29.4-alpine3.23
FROM ${NGINX_IMAGE}
RUN rm -f /etc/nginx/conf.d/default.conf && \
    mkdir -p /var/cache/nginx/client_temp /var/cache/nginx/proxy_temp /var/run/nginx && \
    chown -R 101:101 /var/cache/nginx /var/run/nginx /usr/share/nginx/html
COPY nginx.conf /etc/nginx/nginx.conf
COPY nginx-security-headers.conf /etc/nginx/security-headers.conf
COPY --from=build --chown=101:101 /workspace/dist /usr/share/nginx/html
USER 101:101
EXPOSE 8080
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
