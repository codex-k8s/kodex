---
id: OPS-MC-007
title: Безопасность и секреты
type: operations
status: approved
owner: security
version: 1.1.0
updated: 2026-08-24
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

- GitHub Environment хранит только начальные человеческие пароли; OAuth client,
  cookie, database и service secrets генерируются на exact SHA и передаются
  владельцу только в `age`-encrypted artifact с коротким retention;
- приватный `age` identity не хранится в GitHub, Kubernetes, Vault этой же
  установки или Git и имеет минимум две offline owner-controlled копии;
- Vault использует Shamir `5/3`; plaintext root token и shares существуют
  только внутри ограниченной bootstrap/unseal ceremony и сразу повторно
  шифруются;
- `master/admin` Keycloak является явной cluster-admin authority для Headlamp.
  Назначение и отзыв этой роли рассматриваются как изменение Kubernetes
  cluster-admin доступа и проверяются readback;
- Control Center, Grafana и Vault UI требуют `mattercodex-owner`, но не выдают
  Kubernetes authority. Прямой публичный backend path в обход OAuth2 Proxy
  запрещён.

Полный порядок, secret names и восстановление определяет `RUN-MC-023`.
