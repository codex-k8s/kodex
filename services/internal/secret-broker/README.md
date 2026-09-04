---
id: REPO-MC-021
title: Сервис secret-broker
type: repository-readme
status: approved
owner: backend
version: 1.0.0
updated: 2026-09-04
---

# Сервис secret-broker

`secret-broker` является единственной plaintext boundary для Runtime Secrets и
provider credentials. Авторитетное состояние, ownership, lease, revision,
generation и config binding принадлежат `control-plane`; Kubernetes хранит
только immutable materialization в namespace `kodex-runtime`.

## Интерфейсы

- `SecretBrokerService` выполняет create/rotate/reveal/revoke по одноразовой
  operation grant;
- `RuntimeCredentialProjectionService` материализует exact provider и
  RuntimeSecret sources для одной execution lease;
- `TranscriptionCredentialProjectionService` возвращает API key только для
  exact System STT config/account/credential generation;
- `ProviderCredentialMaterializerService` создаёт и удаляет provider
  credential materialization по owner-командам control-plane.

Projection RPC защищены mTLS, одноразовым internal authorization context и
закрытым operation registry. Runtime projection хранит immutable manifest для
reconciler; STT response не содержит provider JSON и ограничен expiry proof.

## Локальная проверка

```bash
cd services/internal/secret-broker
GOWORK=off go test ./internal/...
```

Contract/codegen и authority policy проверяются из корня:

```bash
make lint-proto build-proto gen-proto check-proto-codegen
make test-authority-policy-codegen
```

Диагностика и безопасное восстановление описаны в
[`docs/runbooks/secret-broker.md`](../../../docs/runbooks/secret-broker.md).
