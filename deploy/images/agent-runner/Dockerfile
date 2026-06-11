FROM node:22-alpine

ARG CODEX_PACKAGE=@openai/codex@0.138.0
ARG MATTERCODEX_AGENT_RUNNER_UID=10001
ARG MATTERCODEX_AGENT_RUNNER_GID=10001

RUN apk add --no-cache git github-cli jq openssh-client \
  && npm install -g "${CODEX_PACKAGE}"

RUN addgroup -g "${MATTERCODEX_AGENT_RUNNER_GID}" matter-codex \
  && adduser -D -H -u "${MATTERCODEX_AGENT_RUNNER_UID}" -G matter-codex matter-codex \
  && mkdir -p /home/matter-codex/.npm /home/matter-codex/.config /home/matter-codex/.cache /workspace /codex-home \
  && chown -R "${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}" /home/matter-codex /workspace /codex-home

ENV HOME=/home/matter-codex \
  NPM_CONFIG_CACHE=/home/matter-codex/.npm \
  XDG_CONFIG_HOME=/home/matter-codex/.config \
  XDG_CACHE_HOME=/home/matter-codex/.cache

WORKDIR /workspace

USER ${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}

CMD ["sh"]
