FROM golang:1.26.5-alpine AS scratch

RUN mkdir -p /out /tmp/mattercodex-go-build-cache \
  && printf '%s' 'cGFja2FnZSBtYWluCgppbXBvcnQgKAoJImJ5dGVzIgoJImZtdCIKCSJvcyIKCSJvcy9leGVjIgopCgpmdW5jIGZhaWwoZm9ybWF0IHN0cmluZywgdmFsdWVzIC4uLmFueSkgewoJZm10LkZwcmludGYob3MuU3RkZXJyLCBmb3JtYXQrIlxuIiwgdmFsdWVzLi4uKQoJb3MuRXhpdCgxKQp9CgpmdW5jIHZlcmlmeShnb0V4ZWN1dGFibGUgc3RyaW5nKSB7CgljaGVja3MgOj0gW11zdHJ1Y3QgewoJCXZhcmlhYmxlIHN0cmluZwoJCWV4cGVjdGVkIHN0cmluZwoJfXsKCQl7dmFyaWFibGU6ICJHT1ZFUlNJT04iLCBleHBlY3RlZDogImdvMS4yNi41XG4ifSwKCQl7dmFyaWFibGU6ICJHT1RPT0xDSEFJTiIsIGV4cGVjdGVkOiAibG9jYWxcbiJ9LAoJfQoJZm9yIF8sIGNoZWNrIDo9IHJhbmdlIGNoZWNrcyB7CgkJb3V0cHV0LCBlcnIgOj0gZXhlYy5Db21tYW5kKGdvRXhlY3V0YWJsZSwgImVudiIsIGNoZWNrLnZhcmlhYmxlKS5PdXRwdXQoKQoJCWlmIGVyciAhPSBuaWwgewoJCQlmYWlsKCIlcyBmYWlsZWQ6ICV2IiwgY2hlY2sudmFyaWFibGUsIGVycikKCQl9CgkJaWYgIWJ5dGVzLkVxdWFsKG91dHB1dCwgW11ieXRlKGNoZWNrLmV4cGVjdGVkKSkgewoJCQlmYWlsKCIlcyBtaXNtYXRjaDogZ290ICVxLCB3YW50ICVxIiwgY2hlY2sudmFyaWFibGUsIG91dHB1dCwgY2hlY2suZXhwZWN0ZWQpCgkJfQoJfQp9CgpmdW5jIG1haW4oKSB7CglpZiBsZW4ob3MuQXJncykgPT0gMiAmJiBvcy5BcmdzWzFdID09ICJjbGVhbiIgewoJCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwoIi91c3IvbG9jYWwvZ28iKTsgZXJyICE9IG5pbCB7CgkJCWZhaWwoImNsZWFuIC91c3IvbG9jYWwvZ286ICV2IiwgZXJyKQoJCX0KCQlpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKCIvb3B0L21hdHRlcmNvZGV4L2Jvb3RzdHJhcC1nbyIpOyBlcnIgIT0gbmlsIHsKCQkJZmFpbCgiY2xlYW4gYm9vdHN0cmFwIEdvOiAldiIsIGVycikKCQl9CgkJcmV0dXJuCgl9CglpZiBsZW4ob3MuQXJncykgPT0gMyAmJiBvcy5BcmdzWzFdID09ICJ2ZXJpZnkiIHsKCQl2ZXJpZnkob3MuQXJnc1syXSkKCQlyZXR1cm4KCX0KCWZhaWwoInVuc3VwcG9ydGVkIGludm9jYXRpb24iKQp9Cg==' | base64 -d > /tmp/mattercodex-go-toolchain-guard.go \
  && /usr/bin/env -i PATH=/usr/local/go/bin:/usr/bin:/bin HOME=/tmp GOCACHE=/tmp/mattercodex-go-build-cache GOENV=off GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 /usr/local/go/bin/go build -trimpath -buildvcs=false -o /out/mattercodex-go-toolchain-guard /tmp/mattercodex-go-toolchain-guard.go \
  && rm -rf /tmp/mattercodex-go-toolchain-guard.go /tmp/mattercodex-go-build-cache

FROM golang:1.26.5-alpine AS context

ENV GOTOOLCHAIN=local

ARG GOOSE_VERSION=v3.27.1
ARG OAPI_CODEGEN_VERSION=v2.7.1
ARG PROTOC_GEN_GO_VERSION=v1.36.11
ARG PROTOC_GEN_GO_GRPC_VERSION=v1.6.2
ARG GOLANGCI_LINT_VERSION=v2.12.2
ARG GOIMPORTS_VERSION=v0.46.0
ARG GOFUMPT_VERSION=v0.10.0
ARG STATICCHECK_VERSION=v0.7.0
ARG BUF_VERSION=v1.71.0
ARG GRPCURL_VERSION=v1.9.3
ARG YQ_VERSION=v4.53.3
ARG SQLC_VERSION=v1.31.1
ARG MOCKGEN_VERSION=v0.6.0

ENV GOBIN=/tool-bin

RUN test "$(go env GOVERSION)" = "go1.26.5"
RUN apk add --no-cache ca-certificates git \
  && mkdir -p "${GOBIN}" \
  && CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@"${GOOSE_VERSION}" \
  && CGO_ENABLED=0 go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@"${OAPI_CODEGEN_VERSION}" \
  && CGO_ENABLED=0 go install google.golang.org/protobuf/cmd/protoc-gen-go@"${PROTOC_GEN_GO_VERSION}" \
  && CGO_ENABLED=0 go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@"${PROTOC_GEN_GO_GRPC_VERSION}" \
  && CGO_ENABLED=0 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@"${GOLANGCI_LINT_VERSION}" \
  && CGO_ENABLED=0 go install golang.org/x/tools/cmd/goimports@"${GOIMPORTS_VERSION}" \
  && CGO_ENABLED=0 go install mvdan.cc/gofumpt@"${GOFUMPT_VERSION}" \
  && CGO_ENABLED=0 go install honnef.co/go/tools/cmd/staticcheck@"${STATICCHECK_VERSION}" \
  && CGO_ENABLED=0 go install github.com/bufbuild/buf/cmd/buf@"${BUF_VERSION}" \
  && CGO_ENABLED=0 go install github.com/fullstorydev/grpcurl/cmd/grpcurl@"${GRPCURL_VERSION}" \
  && CGO_ENABLED=0 go install github.com/mikefarah/yq/v4@"${YQ_VERSION}" \
  && CGO_ENABLED=0 go install github.com/sqlc-dev/sqlc/cmd/sqlc@"${SQLC_VERSION}" \
  && CGO_ENABLED=0 go install go.uber.org/mock/mockgen@"${MOCKGEN_VERSION}"
FROM node:24-alpine

ENV GOTOOLCHAIN=local

ARG CODEX_PACKAGE=@openai/codex@0.144.1
ARG GH_CLI_VERSION=2.95.0
ARG KUBECTL_VERSION=v1.36.2
ARG HELM_VERSION=v4.2.1
ARG TYPESCRIPT_VERSION=6.0.3
ARG VUE_VERSION=3.5.38
ARG CREATE_VUE_VERSION=3.22.4
ARG VITE_VERSION=8.0.16
ARG VUE_TSC_VERSION=3.3.5
ARG VITEST_VERSION=4.1.9
ARG ESLINT_VERSION=10.5.0
ARG PRETTIER_VERSION=3.8.4
ARG PNPM_VERSION=11.8.0
ARG HEY_OPENAPI_TS_VERSION=0.98.2
ARG ASYNCAPI_CLI_VERSION=6.0.2
ARG ASYNCAPI_GENERATOR_VERSION=3.3.0
ARG ASYNCAPI_MODELINA_CLI_VERSION=5.10.1
ARG WSCAT_VERSION=6.1.0
ARG MATTERCODEX_AGENT_RUNNER_UID=10001
ARG MATTERCODEX_AGENT_RUNNER_GID=10001

COPY --from=1 /tool-bin/ /usr/local/bin/

ENV PATH=/opt/mattercodex/bootstrap-go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

COPY --from=0 /usr/local/go/ /opt/mattercodex/bootstrap-go/

RUN apk add --no-cache \
    bash \
    build-base \
    bind-tools \
    ca-certificates \
    curl \
    fd \
    git \
    jq \
    just \
    less \
    make \
    netcat-openbsd \
    openssl \
    openssh-client \
    postgresql-client \
    protobuf \
    redis \
    ripgrep \
    tar \
    tini \
    tree \
  && ln -sf /usr/local/go/bin/go /usr/local/bin/go \
  && ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt \
  && set -eu; \
  case "$(apk --print-arch)" in \
    x86_64) gh_arch=amd64; kubectl_arch=amd64; helm_arch=amd64 ;; \
    aarch64) gh_arch=arm64; kubectl_arch=arm64; helm_arch=arm64 ;; \
    *) echo "unsupported architecture: $(apk --print-arch)" >&2; exit 1 ;; \
  esac; \
  curl -fsSL -o /tmp/gh.tar.gz "https://github.com/cli/cli/releases/download/v${GH_CLI_VERSION}/gh_${GH_CLI_VERSION}_linux_${gh_arch}.tar.gz"; \
  tar -xzf /tmp/gh.tar.gz -C /tmp; \
  mv "/tmp/gh_${GH_CLI_VERSION}_linux_${gh_arch}/bin/gh" /usr/local/bin/gh; \
  curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${kubectl_arch}/kubectl"; \
  curl -fsSLo /tmp/kubectl.sha256 "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${kubectl_arch}/kubectl.sha256"; \
  echo "$(cat /tmp/kubectl.sha256)  /usr/local/bin/kubectl" | sha256sum -c -; \
  curl -fsSL -o "/tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz" "https://get.helm.sh/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz"; \
  curl -fsSL -o "/tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum" "https://get.helm.sh/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum"; \
  (cd /tmp && sha256sum -c "helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum"); \
  tar -xzf "/tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz" -C /tmp; \
  mv "/tmp/linux-${helm_arch}/helm" /usr/local/bin/helm; \
  chmod +x /usr/local/bin/gh /usr/local/bin/kubectl /usr/local/bin/helm; \
  rm -rf /tmp/gh.tar.gz /tmp/gh_${GH_CLI_VERSION}_linux_${gh_arch} /tmp/kubectl.sha256 /tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz /tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum /tmp/linux-${helm_arch} \
  && npm install -g \
    "${CODEX_PACKAGE}" \
    "typescript@${TYPESCRIPT_VERSION}" \
    "vue@${VUE_VERSION}" \
    "create-vue@${CREATE_VUE_VERSION}" \
    "vite@${VITE_VERSION}" \
    "vue-tsc@${VUE_TSC_VERSION}" \
    "vitest@${VITEST_VERSION}" \
    "eslint@${ESLINT_VERSION}" \
    "prettier@${PRETTIER_VERSION}" \
    "pnpm@${PNPM_VERSION}" \
    "@hey-api/openapi-ts@${HEY_OPENAPI_TS_VERSION}" \
    "@asyncapi/cli@${ASYNCAPI_CLI_VERSION}" \
    "@asyncapi/generator@${ASYNCAPI_GENERATOR_VERSION}" \
    "@asyncapi/modelina-cli@${ASYNCAPI_MODELINA_CLI_VERSION}" \
    "wscat@${WSCAT_VERSION}" \
  && node --version \
  && npm --version \
  && go version \
  && gh --version >/dev/null \
  && kubectl version --client=true >/dev/null \
  && helm version --short >/dev/null \
  && psql --version >/dev/null \
  && redis-cli --version >/dev/null \
  && jq --version >/dev/null \
  && yq --version >/dev/null \
  && rg --version >/dev/null \
  && fd --version >/dev/null \
  && just --version >/dev/null \
  && goose --version >/dev/null \
  && oapi-codegen -version >/dev/null \
  && protoc --version >/dev/null \
  && protoc-gen-go --version >/dev/null \
  && protoc-gen-go-grpc --version >/dev/null \
  && command -v goimports >/dev/null \
  && command -v gofumpt >/dev/null \
  && staticcheck -version >/dev/null \
  && buf --version >/dev/null \
  && grpcurl --version >/dev/null \
  && sqlc version >/dev/null \
  && mockgen -version >/dev/null \
  && golangci-lint version >/dev/null \
  && tsc --version >/dev/null \
  && command -v create-vue >/dev/null \
  && vite --version >/dev/null \
  && vue-tsc --version >/dev/null \
  && vitest --version >/dev/null \
  && eslint --version >/dev/null \
  && prettier --version >/dev/null \
  && pnpm --version >/dev/null \
  && yarn --version >/dev/null \
  && command -v openapi-ts >/dev/null \
  && command -v asyncapi >/dev/null \
  && command -v modelina >/dev/null \
  && command -v wscat >/dev/null \
  && codex --version >/dev/null

RUN addgroup -g "${MATTERCODEX_AGENT_RUNNER_GID}" matter-codex \
  && adduser -D -H -u "${MATTERCODEX_AGENT_RUNNER_UID}" -G matter-codex matter-codex \
  && mkdir -p /home/matter-codex/.npm /home/matter-codex/.config /home/matter-codex/.cache /workspace /codex-home \
  && chown -R "${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}" /home/matter-codex /workspace /codex-home

ENV HOME=/home/matter-codex \
  NPM_CONFIG_CACHE=/home/matter-codex/.npm \
  XDG_CONFIG_HOME=/home/matter-codex/.config \
  XDG_CACHE_HOME=/home/matter-codex/.cache

WORKDIR /workspace

COPY --from=0 /out/mattercodex-go-toolchain-guard /usr/local/libexec/mattercodex-go-toolchain-guard
RUN ["/usr/local/libexec/mattercodex-go-toolchain-guard", "clean"]
COPY --from=0 /usr/local/go/ /usr/local/go/
RUN ["/usr/local/libexec/mattercodex-go-toolchain-guard", "verify", "/usr/local/go/bin/go"]

ENV PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

USER ${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}

CMD ["sh"]
