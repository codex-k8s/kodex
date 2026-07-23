FROM golang:1.26.5-alpine AS scratch

RUN mkdir -p /out /tmp/mattercodex-go-build-cache \
	&& printf '%s' 'cGFja2FnZSBtYWluCgppbXBvcnQgKAoJImJ5dGVzIgoJImZtdCIKCSJvcyIKCSJvcy9leGVjIgopCgpjb25zdCB0cnVzdGVkR09ST09UID0gIi91c3IvbG9jYWwvZ28iCgpmdW5jIGZhaWwoZm9ybWF0IHN0cmluZywgdmFsdWVzIC4uLmFueSkgewoJZm10LkZwcmludGYob3MuU3RkZXJyLCBmb3JtYXQrIlxuIiwgdmFsdWVzLi4uKQoJb3MuRXhpdCgxKQp9CgpmdW5jIHRydXN0ZWRHb0NvbW1hbmQoZ29FeGVjdXRhYmxlIHN0cmluZywgYXJndW1lbnRzIC4uLnN0cmluZykgKmV4ZWMuQ21kIHsKCWNvbW1hbmQgOj0gZXhlYy5Db21tYW5kKGdvRXhlY3V0YWJsZSwgYXJndW1lbnRzLi4uKQoJY29tbWFuZC5FbnYgPSBbXXN0cmluZ3sKCQkiUEFUSD0vdXNyL2xvY2FsL2dvL2JpbjovdXNyL2JpbjovYmluIiwKCQkiSE9NRT0vdG1wIiwKCQkiR09ST09UPSIgKyB0cnVzdGVkR09ST09ULAoJCSJHT0VOVj1vZmYiLAoJCSJHT0ZMQUdTPSIsCgkJIkdPVE9PTENIQUlOPWxvY2FsIiwKCQkiR09XT1JLPW9mZiIsCgl9CglyZXR1cm4gY29tbWFuZAp9CgpmdW5jIGV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsIGxhYmVsLCBleHBlY3RlZCBzdHJpbmcsIGFyZ3VtZW50cyAuLi5zdHJpbmcpIHsKCW91dHB1dCwgZXJyIDo9IHRydXN0ZWRHb0NvbW1hbmQoZ29FeGVjdXRhYmxlLCBhcmd1bWVudHMuLi4pLk91dHB1dCgpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCIlcyBmYWlsZWQ6ICV2IiwgbGFiZWwsIGVycikKCX0KCWlmICFieXRlcy5FcXVhbChvdXRwdXQsIFtdYnl0ZShleHBlY3RlZCkpIHsKCQlmYWlsKCIlcyBtaXNtYXRjaDogZ290ICVxLCB3YW50ICVxIiwgbGFiZWwsIG91dHB1dCwgZXhwZWN0ZWQpCgl9Cn0KCmZ1bmMgdmVyaWZ5KGdvRXhlY3V0YWJsZSBzdHJpbmcpIHsKCWV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsICJHT1ZFUlNJT04iLCAiZ28xLjI2LjVcbiIsICJlbnYiLCAiR09WRVJTSU9OIikKCWV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsICJHT1RPT0xDSEFJTiIsICJsb2NhbFxuIiwgImVudiIsICJHT1RPT0xDSEFJTiIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiR09ST09UIiwgdHJ1c3RlZEdPUk9PVCsiXG4iLCAiZW52IiwgIkdPUk9PVCIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiZ28gdG9vbCBjb21waWxlIiwgImNvbXBpbGUgdmVyc2lvbiBnbzEuMjYuNVxuIiwgInRvb2wiLCAiY29tcGlsZSIsICItVj1mdWxsIikKfQoKZnVuYyBtYWluKCkgewoJaWYgbGVuKG9zLkFyZ3MpID09IDIgJiYgb3MuQXJnc1sxXSA9PSAiY2xlYW4iIHsKCQlpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKCIvdXNyL2xvY2FsL2dvIik7IGVyciAhPSBuaWwgewoJCQlmYWlsKCJjbGVhbiAvdXNyL2xvY2FsL2dvOiAldiIsIGVycikKCQl9CgkJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbCgiL29wdC9tYXR0ZXJjb2RleC9ib290c3RyYXAtZ28iKTsgZXJyICE9IG5pbCB7CgkJCWZhaWwoImNsZWFuIGJvb3RzdHJhcCBHbzogJXYiLCBlcnIpCgkJfQoJCXJldHVybgoJfQoJaWYgbGVuKG9zLkFyZ3MpID09IDMgJiYgb3MuQXJnc1sxXSA9PSAidmVyaWZ5IiB7CgkJdmVyaWZ5KG9zLkFyZ3NbMl0pCgkJcmV0dXJuCgl9CglmYWlsKCJ1bnN1cHBvcnRlZCBpbnZvY2F0aW9uIikKfQo=' | base64 -d > /tmp/mattercodex-go-toolchain-guard.go \
	&& /usr/bin/env -i PATH=/usr/local/go/bin:/usr/bin:/bin HOME=/tmp GOCACHE=/tmp/mattercodex-go-build-cache GOENV=off GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 /usr/local/go/bin/go build -trimpath -buildvcs=false -o /out/mattercodex-go-toolchain-guard /tmp/mattercodex-go-toolchain-guard.go \
	&& rm -rf /tmp/mattercodex-go-toolchain-guard.go /tmp/mattercodex-go-build-cache

FROM golang:1.26.5-alpine AS context

ENV GOTOOLCHAIN=local

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY services/jobs/agent-runner ./services/jobs/agent-runner

RUN test "$(go env GOVERSION)" = "go1.26.5"
RUN CGO_ENABLED=0 go build -trimpath -o /out/matter-codex-agent-runner ./services/jobs/agent-runner/cmd/agent-runner

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
FROM node:24-bookworm

ENV GOTOOLCHAIN=local

ARG MATTERCODEX_CODEX_PACKAGE=@openai/codex@0.144.1
ARG GH_CLI_VERSION=2.95.0
ARG KUBECTL_VERSION=v1.36.2
ARG HELM_VERSION=v4.2.1
ARG JUST_VERSION=1.55.1
ARG TYPESCRIPT_VERSION=6.0.3
ARG VUE_VERSION=3.5.38
ARG CREATE_VUE_VERSION=3.22.4
ARG VITE_VERSION=8.0.16
ARG VUE_TSC_VERSION=3.3.5
ARG VITEST_VERSION=4.1.9
ARG ESLINT_VERSION=10.5.0
ARG PRETTIER_VERSION=3.8.4
ARG PNPM_VERSION=11.8.0
ARG YARN_VERSION=1.22.22
ARG HEY_OPENAPI_TS_VERSION=0.98.2
ARG ASYNCAPI_CLI_VERSION=6.0.2
ARG ASYNCAPI_GENERATOR_VERSION=3.3.0
ARG ASYNCAPI_MODELINA_CLI_VERSION=5.10.1
ARG WSCAT_VERSION=6.1.0
ARG PLAYWRIGHT_VERSION=1.61.1
ARG PLAYWRIGHT_MCP_VERSION=0.0.77
ARG WAIT_ON_VERSION=9.0.10
ARG MATTERCODEX_AGENT_RUNNER_UID=10001
ARG MATTERCODEX_AGENT_RUNNER_GID=10001

COPY --from=1 /tool-bin/ /usr/local/bin/
COPY --from=1 /out/matter-codex-agent-runner /usr/local/bin/matter-codex-agent-runner

ENV PATH=/opt/mattercodex/bootstrap-go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin \
	PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

COPY --from=0 /usr/local/go/ /opt/mattercodex/bootstrap-go/

RUN set -eu; \
	apt-get update; \
	apt-get install -y --no-install-recommends \
		bash \
		build-essential \
		ca-certificates \
		chromium \
		curl \
		dnsutils \
		fd-find \
		git \
		jq \
		less \
		make \
		netcat-openbsd \
		openssl \
		openssh-client \
		postgresql-client \
		protobuf-compiler \
		redis-tools \
		ripgrep \
		tar \
		tini \
		tree; \
	ln -sf /usr/bin/tini /sbin/tini; \
	ln -sf /usr/bin/fdfind /usr/local/bin/fd; \
	ln -sf /usr/bin/chromium /usr/local/bin/chromium; \
	ln -sf /usr/local/go/bin/go /usr/local/bin/go; \
	ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt; \
	case "$(dpkg --print-architecture)" in \
		x86_64) gh_arch=amd64; kubectl_arch=amd64; helm_arch=amd64; just_arch=x86_64 ;; \
		amd64) gh_arch=amd64; kubectl_arch=amd64; helm_arch=amd64; just_arch=x86_64 ;; \
		arm64) gh_arch=arm64; kubectl_arch=arm64; helm_arch=arm64; just_arch=aarch64 ;; \
		aarch64) gh_arch=arm64; kubectl_arch=arm64; helm_arch=arm64; just_arch=aarch64 ;; \
		*) echo "unsupported architecture: $(dpkg --print-architecture)" >&2; exit 1 ;; \
	esac; \
	curl_flags="--ipv4 --fail --show-error --silent --location --retry 5 --retry-all-errors --connect-timeout 20 --max-time 300"; \
	curl ${curl_flags} -o /tmp/gh.tar.gz "https://github.com/cli/cli/releases/download/v${GH_CLI_VERSION}/gh_${GH_CLI_VERSION}_linux_${gh_arch}.tar.gz"; \
	tar -xzf /tmp/gh.tar.gz -C /tmp; \
	mv "/tmp/gh_${GH_CLI_VERSION}_linux_${gh_arch}/bin/gh" /usr/local/bin/gh; \
	curl ${curl_flags} -o /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${kubectl_arch}/kubectl"; \
	curl ${curl_flags} -o /tmp/kubectl.sha256 "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${kubectl_arch}/kubectl.sha256"; \
	echo "$(cat /tmp/kubectl.sha256)  /usr/local/bin/kubectl" | sha256sum -c -; \
	curl ${curl_flags} -o "/tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz" "https://get.helm.sh/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz"; \
	curl ${curl_flags} -o "/tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum" "https://get.helm.sh/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum"; \
	(cd /tmp && sha256sum -c "helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum"); \
	tar -xzf "/tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz" -C /tmp; \
	mv "/tmp/linux-${helm_arch}/helm" /usr/local/bin/helm; \
	curl ${curl_flags} -o "/tmp/just.tar.gz" "https://github.com/casey/just/releases/download/${JUST_VERSION}/just-${JUST_VERSION}-${just_arch}-unknown-linux-musl.tar.gz"; \
	tar -xzf /tmp/just.tar.gz -C /tmp just; \
	mv /tmp/just /usr/local/bin/just; \
	chmod +x /usr/local/bin/gh /usr/local/bin/kubectl /usr/local/bin/helm /usr/local/bin/just; \
	rm -rf /tmp/gh.tar.gz /tmp/gh_${GH_CLI_VERSION}_linux_${gh_arch} /tmp/kubectl.sha256 /tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz /tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum /tmp/linux-${helm_arch} /tmp/just.tar.gz; \
	if command -v yarn >/dev/null 2>&1 && [ "$(yarn --version)" = "${YARN_VERSION}" ]; then \
		echo "yarn ${YARN_VERSION} already installed"; \
	else \
		rm -f /usr/local/bin/yarn /usr/local/bin/yarnpkg; \
		npm install -g "yarn@${YARN_VERSION}"; \
	fi; \
	npm install -g \
		"${MATTERCODEX_CODEX_PACKAGE}" \
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
		"playwright@${PLAYWRIGHT_VERSION}" \
		"@playwright/test@${PLAYWRIGHT_VERSION}" \
		"@playwright/mcp@${PLAYWRIGHT_MCP_VERSION}" \
		"wait-on@${WAIT_ON_VERSION}"; \
	playwright install --with-deps chromium; \
	chmod -R a+rX "${PLAYWRIGHT_BROWSERS_PATH}"; \
	node --version; \
	npm --version; \
	go version; \
	gh --version >/dev/null; \
	kubectl version --client=true >/dev/null; \
	helm version --short >/dev/null; \
	psql --version >/dev/null; \
	redis-cli --version >/dev/null; \
	jq --version >/dev/null; \
	yq --version >/dev/null; \
	rg --version >/dev/null; \
	fd --version >/dev/null; \
	just --version >/dev/null; \
	goose --version >/dev/null; \
	oapi-codegen -version >/dev/null; \
	protoc --version >/dev/null; \
	protoc-gen-go --version >/dev/null; \
	protoc-gen-go-grpc --version >/dev/null; \
	command -v goimports >/dev/null; \
	command -v gofumpt >/dev/null; \
	staticcheck -version >/dev/null; \
	buf --version >/dev/null; \
	grpcurl --version >/dev/null; \
	sqlc version >/dev/null; \
	mockgen -version >/dev/null; \
	golangci-lint version >/dev/null; \
	tsc --version >/dev/null; \
	command -v create-vue >/dev/null; \
	vite --version >/dev/null; \
	vue-tsc --version >/dev/null; \
	vitest --version >/dev/null; \
	eslint --version >/dev/null; \
	prettier --version >/dev/null; \
	pnpm --version >/dev/null; \
	yarn --version >/dev/null; \
	command -v openapi-ts >/dev/null; \
	command -v asyncapi >/dev/null; \
	command -v modelina >/dev/null; \
	command -v wscat >/dev/null; \
	chromium --version >/dev/null; \
	playwright --version >/dev/null; \
	command -v playwright-mcp >/dev/null; \
	wait-on --version >/dev/null; \
	codex --version >/dev/null; \
	rm -rf /var/lib/apt/lists/* /root/.cache

RUN groupadd --gid "${MATTERCODEX_AGENT_RUNNER_GID}" matter-codex \
	&& useradd --uid "${MATTERCODEX_AGENT_RUNNER_UID}" --gid "${MATTERCODEX_AGENT_RUNNER_GID}" --no-create-home --shell /usr/sbin/nologin matter-codex \
	&& mkdir -p /home/matter-codex/.npm /home/matter-codex/.config /home/matter-codex/.cache /workspace /codex-home \
	&& chown -R "${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}" /home/matter-codex /workspace /codex-home

ENV HOME=/home/matter-codex \
	NPM_CONFIG_CACHE=/home/matter-codex/.npm \
	XDG_CONFIG_HOME=/home/matter-codex/.config \
	XDG_CACHE_HOME=/home/matter-codex/.cache

COPY --from=1 /tool-bin/ /usr/local/bin/
COPY --from=0 /out/mattercodex-go-toolchain-guard /usr/local/libexec/mattercodex-go-toolchain-guard
RUN ["/usr/local/libexec/mattercodex-go-toolchain-guard", "clean"]
COPY --from=0 /usr/local/go/ /usr/local/go/
ENV GOROOT=/usr/local/go \
	GOENV=off \
	GOFLAGS= \
	GOTOOLCHAIN=local \
	PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin \
	PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
RUN ["/usr/local/libexec/mattercodex-go-toolchain-guard", "verify", "/usr/local/go/bin/go"]

USER ${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}

ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/matter-codex-agent-runner"]
