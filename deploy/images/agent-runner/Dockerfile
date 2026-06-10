FROM node:22-alpine

ARG CODEX_PACKAGE=@openai/codex@0.138.0

RUN apk add --no-cache git github-cli jq openssh-client \
  && npm install -g "${CODEX_PACKAGE}"

WORKDIR /workspace

CMD ["sh"]
