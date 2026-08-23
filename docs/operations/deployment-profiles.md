---
id: OPS-MC-002
title: Профили развертывания
type: operations
status: approved
owner: sre
version: 1.1.0
updated: 2026-08-23
---

# Профили развертывания

## `web-only`

`deploy/k8s/profiles/web-only` — основной и самодостаточный профиль. Он включает:

- `staff-control-center` и `control-api-gateway`;
- `platform-state`: fresh PostgreSQL для `control-plane` и internal RPC
  authority, NATS JetStream, server/client TLS и exact NetworkPolicy;
- `control-plane` с единой fresh baseline schema;
- transactional outbox relay;
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

`platform-state` не фиксирует installation-specific `StorageClass`: PVC
используют default class выбранного кластера. Shipped baseline использует один
PostgreSQL Pod и один NATS Pod, поэтому stream replication factor равен `1`.
Замена их на внешние HA endpoints допускается только при сохранении exact DNS,
TLS, database role и NATS account contracts; значение replication factor тогда
материализуется тем же release profile, а не меняется вручную после render.

Manifest не содержит secret values. До применения StatefulSet владелец
материализует `mattercodex-installation-ca`,
`mattercodex-postgresql-bootstrap` и `mattercodex-nats-credentials` по
`RUN-MC-002`. CA bundle переносится в клиентские ConfigMap через trust-manager;
клиентские Pod не получают server private key.

## Render

Release lock создаёт `tools/release/build-release.sh`. Поддерживаемый manifest
рендерит `tools/release/render-web-only.sh` с `--profile web-only` либо
`--profile web-with-mattermost`. Несмотря на историческое имя скрипта, параметр
профиля является авторитетным.

`build-release.sh` первым собирает закреплённый в репозитории
`infra/admission-tools/Dockerfile`, получает digest из BuildKit metadata и
только затем собирает остальные образы. Внешняя переменная с произвольным
tools image не используется: build dependency и runtime pull reference
материализуются из одного digest в том же release lock.

Для `web-with-mattermost` render дополнительно требует
`--mattermost-host <exact-dns>`. Он одновременно материализует installation-level
allowlist interaction adapter и тот же exact destination в egress policy. В
`web-only` этот параметр запрещён. Release workflow читает значение только из
installation variable `MATTERCODEX_MATTERMOST_HOST` и не хранит домен в коде.

Render не применяет ресурсы в кластер. Применение, reset и deploy выполняет
только владелец после отдельного решения.
