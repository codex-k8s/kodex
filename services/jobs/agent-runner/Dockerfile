FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY services/jobs/agent-runner ./services/jobs/agent-runner

RUN CGO_ENABLED=0 GO111MODULE=off go build -trimpath -o /out/matter-codex-agent-runner ./services/jobs/agent-runner/cmd/agent-runner

FROM node:22-alpine

ARG MATTERCODEX_CODEX_PACKAGE=@openai/codex@0.138.0
ARG MATTERCODEX_AGENT_RUNNER_UID=10001
ARG MATTERCODEX_AGENT_RUNNER_GID=10001

RUN apk add --no-cache ca-certificates git github-cli \
	&& npm install -g "${MATTERCODEX_CODEX_PACKAGE}"

COPY --from=builder /out/matter-codex-agent-runner /usr/local/bin/matter-codex-agent-runner

RUN addgroup -g "${MATTERCODEX_AGENT_RUNNER_GID}" matter-codex \
	&& adduser -D -H -u "${MATTERCODEX_AGENT_RUNNER_UID}" -G matter-codex matter-codex \
	&& mkdir -p /home/matter-codex/.npm /home/matter-codex/.config /home/matter-codex/.cache /workspace /codex-home \
	&& chown -R "${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}" /home/matter-codex /workspace /codex-home

ENV HOME=/home/matter-codex \
	NPM_CONFIG_CACHE=/home/matter-codex/.npm \
	XDG_CONFIG_HOME=/home/matter-codex/.config \
	XDG_CACHE_HOME=/home/matter-codex/.cache

USER ${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}

ENTRYPOINT ["/usr/local/bin/matter-codex-agent-runner"]
