---
id: OPS-MC-002
title: Профили развертывания
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-23
---

# Профили развертывания

## `web-only`

`deploy/k8s/profiles/web-only` — основной и самодостаточный профиль. Он включает:

- `staff-control-center` и `control-api-gateway`;
- `control-plane` с единой fresh baseline PostgreSQL;
- NATS JetStream и transactional outbox relay;
- `runtime-controller`, `agent-runner` ABI и always-hot system assistant;
- `role-image-builder` и image admission/supply chain;
- `integration-gateway`, готовый при нуле connections и credentials;
- `automation-scheduler`, OIDC, internal RPC authority и platform egress.

Mattermost, Git providers и внешние business systems отсутствуют и не входят в
startup/readiness. Kubernetes является средой выполнения самой платформы и
role Pod, но Kubernetes connection как пользовательская интеграция не обязателен.

## `web-with-mattermost`

`deploy/k8s/profiles/web-with-mattermost` расширяет `web-only` отдельным
`interaction-gateway`. Четыре capabilities включаются независимо:

- inbound messages;
- notifications;
- result mirror;
- Human Gate decisions.

Профиль не содержит server URL, домен, Team/Channel ID или credentials в коде.
Exact allowed hosts и credential references материализуются владельцем перед
развертыванием. Недоступность Mattermost отражается как отдельный delivery
incident и не меняет core Run outcome.

## Общие инварианты

- все внутренние images закреплены по `repository@sha256` в release lock;
- mutable tag, zero digest и template placeholder закрыто отклоняются;
- public host, origin, OIDC endpoints, registry и Kubernetes API CIDR являются
  параметрами render, а не hardcoded значениями;
- прямые инфраструктурные зависимости Pod: PostgreSQL, NATS, локальный storage,
  issuer/verifier sidecar и egress boundary;
- соседний бизнес-сервис не входит в Kubernetes readiness;
- `/healthz` означает жизнь процесса, `/readyz` читает локальный рассчитанный
  snapshot без сетевого fan-out;
- полный service graph проверяется после rollout отдельной диагностикой;
- role image build и platform release build используют изолированные
  identities и разные promotion credentials.

## Render

Release lock создаёт `tools/release/build-release.sh`. Поддерживаемый manifest
рендерит `tools/release/render-web-only.sh` с `--profile web-only` либо
`--profile web-with-mattermost`. Несмотря на историческое имя скрипта, параметр
профиля является авторитетным.

Render не применяет ресурсы в кластер. Применение, reset и deploy выполняет
только владелец после отдельного решения.
