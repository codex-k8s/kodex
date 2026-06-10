FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY services/jobs/agent-runner ./services/jobs/agent-runner

RUN CGO_ENABLED=0 GO111MODULE=off go build -trimpath -o /out/matter-codex-agent-runner ./services/jobs/agent-runner/cmd/agent-runner

FROM node:22-alpine

ARG MATTERCODEX_CODEX_PACKAGE=@openai/codex@0.138.0

RUN apk add --no-cache ca-certificates git github-cli \
	&& npm install -g "${MATTERCODEX_CODEX_PACKAGE}"

COPY --from=builder /out/matter-codex-agent-runner /usr/local/bin/matter-codex-agent-runner

ENTRYPOINT ["/usr/local/bin/matter-codex-agent-runner"]
