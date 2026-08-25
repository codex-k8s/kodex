---
id: OPS-MC-007
title: Безопасность и секреты
type: operations
status: approved
owner: security
version: 2.0.0
updated: 2026-08-25
---

# Безопасность и секреты

## Authority

- OIDC identity разрешается в Organization/Membership на сервере;
- browser payload не является источником actor, owner, project или lineage;
- mTLS подтверждает transport peer, но не заменяет application token, exact
  operation, permission, fence и replay protection;
- чужой или скрытый opaque ref отклоняется тем же owner eligibility rule;
- RuntimeRevision pin-ит exact image, instructions, grants, artifacts, input
  digest, attempt и generation.

## Секреты

Secret value принимается только доверенным credential boundary, хранится в
secret storage и возвращается как masked state. Значение запрещено в Git,
ConfigMap, prompt, audit, log, trace, metric, event, frontend JSON, artifact и
raw provider error.

Role Pod не получает credentials управляемой интеграции. `integration-gateway`
выполняет только зарегистрированную typed MCP capability после server-owned
grant/approval. Прямой credential разрешается только явно выбранной role policy,
если обход Human Gate допустим по модели риска.

## Role image и Kubernetes

- `role-image-builder` не получает runtime secrets;
- build, scan, sign, promotion и node pull имеют разные identities;
- runtime запускает только promoted `repository@sha256` с совместимым ABI;
- execution Pod получает минимальный ServiceAccount, read-only protected runtime
  material и exact network egress;
- Kubernetes ID, namespace, ServiceAccount или external connection locator не
  является domain authority.

## Key lifecycle

Key и CA rotation является forward-only протоколом с overlap и exact readback.
Replay/rollback high watermark хранит verifier side, а не caller. Повреждение
signature, конфликт revision, expiry или истечение grace немедленно закрывают
доступ; только краткий сетевой отказ может использовать bounded LKG.

Материализация выполняется code-first после owner approval и никогда не печатает
значения. PR содержит только имена expected keys и проверку формы.

## Identity и recovery

- `.kodex-env` хранится только у владельца с mode `0600`, не коммитится и не
  передаётся через artifact;
- OAuth client, cookie, database, NATS, registry и service secrets генерируются
  локально в `.kodex-material`, материализуются в Kubernetes Secrets и не
  печатаются;
- k3s включает encryption at rest для Kubernetes Secrets;
- `.kodex-material` имеет минимум две зашифрованные offline owner-controlled
  копии, отделённые от PostgreSQL/PVC backup;
- потеря installation roots при существующих данных считается инцидентом;
  молчаливая генерация новой identity поверх работающей установки запрещена;
- `master/admin` Keycloak является явной cluster-admin authority для Headlamp.
  Назначение и отзыв этой роли рассматриваются как изменение Kubernetes
  cluster-admin доступа и проверяются readback;
- Control Center и Grafana требуют `kodex-owner`, но не выдают
  Kubernetes authority. Прямой публичный backend path в обход OAuth2 Proxy
  запрещён.

Полный порядок, secret names и восстановление определяет `RUN-MC-023`.
