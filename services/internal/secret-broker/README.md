---
id: REPO-MC-021
title: Сервис secret-broker
type: repository-readme
status: approved
owner: backend
version: 1.2.0
updated: 2026-09-05
---

# Сервис secret-broker

`secret-broker` является единственной plaintext boundary для Runtime Secrets и
provider credentials. Авторитетное состояние, ownership, lease, revision,
generation и config binding принадлежат `control-plane`; опубликованные
immutable значения находятся в `kodex-runtime`, зашифрованные черновики — в
отдельном `kodex-secret-drafts`. Keyring проецируется только в broker из
`kodex-system` и не входит в execution projection.

## Интерфейсы

- `SecretBrokerService` выполняет create/rotate/reveal/revoke по одноразовой
  operation grant;
- его save/validate/publish/discard draft команды используют полный protected
  путь с one-time grant и безопасным owner readback без plaintext в metadata;
- `RuntimeCredentialProjectionService` материализует exact provider и
  RuntimeSecret sources для одной execution lease;
- `TranscriptionCredentialProjectionService` возвращает API key только для
  exact System STT config/account/credential generation;
- `ProviderCredentialMaterializerService` создаёт и удаляет provider
  credential materialization по owner-командам control-plane; отдельный
  `ObserveProviderModelCatalog` возвращает только безопасные возможности
  моделей для exact account/credential task, без credential или provider payload.

Projection RPC защищены mTLS, одноразовым internal authorization context и
закрытым operation registry. Runtime projection хранит immutable manifest для
reconciler; STT response не содержит provider JSON и ограничен expiry proof.

## Локальная проверка

```bash
make test-secret-broker-drafts
```

Contract/codegen и authority policy проверяются из корня:

```bash
make lint-proto build-proto gen-proto check-proto-codegen
make test-authority-policy-codegen
```

Диагностика и безопасное восстановление описаны в
[`docs/runbooks/secret-broker.md`](../../../docs/runbooks/secret-broker.md).

Жизненный цикл черновика, bootstrap/rotation и ограничения checkpoint — в
[`secret-drafts-1068.md`](../../../docs/operations/secret-drafts-1068.md).

Полномочия, источник и ограничения каталога моделей описаны в
[`provider-model-catalog-1068.md`](../../../docs/operations/provider-model-catalog-1068.md).
