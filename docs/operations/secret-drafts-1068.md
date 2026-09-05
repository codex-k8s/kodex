---
id: OPS-SECRET-DRAFTS-1068
title: Защищённые черновики Runtime Secret
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-05
---

# Граница поставки

Issue #1068 завершает Secret Broker часть D6 #1046 для MVP-UI-46/47/49/50.
Control-plane владеет metadata, tenant/owner, draft generation, OCC,
идемпотентностью, lease/claim и конечным состоянием. Secret Broker принимает
plaintext только в ограниченном RPC save и публикует его только в разрешённую
immutable runtime materialization. Сохранённый черновик содержит ciphertext;
ключевой материал доставляется отдельно только broker.

## Карта сценариев и жизненного цикла

| Действие | Полномочия и переход владельца | Эффект broker | Итог и восстановление |
| --- | --- | --- | --- |
| Save | HTTP session/CSRF → CP PrepareSave с project ownership, expected version и idempotency → отдельный одноразовый SAVE grant | Bounded plaintext/type/digest validation, AES-256-GCM с random nonce и exact immutable AAD, encrypted Kubernetes Secret | Owner Complete фиксирует descriptor; active Secret не меняется. Lost reply возвращает прежний exact intent. |
| Validate | CP разрешает текущий draft и выдаёт отдельный VALIDATE grant | Exact UID/resourceVersion/ciphertext/key identity readback, decrypt и type/content validation | Owner фиксирует VALID только для той же generation. Plaintext не возвращается. |
| Publish | Fresh owner/tenant authority, validated generation, expected active version, отдельный PUBLISH claim | Exact encrypted read, decrypt, fenced immutable runtime materialization | CP атомарно фиксирует active revision/audit/receipt; effect-before-complete восстанавливается по exact descriptor. |
| Discard | CP закрывает draft, grants и claims в owner-транзакции | Bounded удаление только exact UID/resourceVersion | Повтор после lost response не публикует и не удаляет чужой объект. |
| Expiry | Серверное время и точный незавершённый draft, закрытие owner-графа | Точная очистка закрытого encrypted descriptor | Новая replica продолжает по authoritative owner read; локальный таймер не назначает terminal. |
| Claim expiry/retry | Предыдущий lease закрыт до новой claim generation | Readback прежнего эффекта перед новым действием | Неизвестный исход не подменяется новым idempotency key. |
| Key rotation | Отдельная repo-owned операция доставки, монотонная generation | Read-only keyring, durable rollback guard, overlap для сохранённых черновиков | Missing/corrupt/retired key закрыто отклоняет decrypt/readiness; прежние drafts не удаляются. |

Публичные endpoints и подготовка операций принадлежат #1045/#1046; browser не
получает operation grant, encrypted descriptor или keyring. Authority RPC,
клиент, policy registration, readiness и итоговый render входят в полную
поставку; наличие типов само по себе не означает завершения.

Для каждой terminal операции отдельного доменного события не требуется:
авторитетный путь — CP draft/tombstone/operation receipt, а broker recovery
читает exact owner work. Published Runtime Secret использует существующий
owner revision/impact/rebind путь. Потребители не читают staging напрямую.
Предварительный immutable impact plan и selective rebind до публикации
реализуются владельцем #1046 и потребляются #1045/#1022; один broker RPC не
является доказательством завершения MVP-UI-47.

## Развёртывание и восстановление ключей

Оба профиля включают `deploy/k8s/base/runtime-secret-drafts` один раз.
Зашифрованные immutable объекты находятся в `kodex-secret-drafts`; там нет
workload и действует deny-all NetworkPolicy. Broker получает только
`get/create/delete` Secrets этого namespace и `get/update` одного ConfigMap
`secret-broker-draft-key-guard`. Keyring Secret `secret-broker-draft-keyring`
находится в `kodex-system` и проецируется read-only только в контейнер broker,
без `subPath`. Доступ к прочим platform Secrets через этот Role не появляется.

`generate-material.sh` создаёт новый installation keyring. Его material и
projection files остаются в owner-private installation directory.
`materialize-secrets.sh` передаёт этот единственный Secret отдельному
`bootstrap-secret-drafts.sh ensure`; общий declarative apply его не обновляет.
Genesis guard создаётся только при отсутствии и keyring, и guard. Существующий
guard никогда не сбрасывается. Удалённый guard при сохранённом keyring требует
восстановления точного защищённого backup, а не повторной инициализации.

Для согласованной ротации используются только repo-owned команды. Переменные
с путями указывают на private файлы, `EXACT_CONTEXT` — на разрешённый disposable
контур. Файлы keyring не печатаются, не передаются в GitHub и не становятся
артефактом проверки.

```bash
cd services/internal/secret-broker
go run ./cmd/secret-draft-keys rotate \
  --input-file "$CURRENT_KEYRING_FILE" --output-file "$NEXT_KEYRING_FILE" \
  --expected-revision "$EXPECTED_KEYRING_REVISION"
cd ../../..
./tools/install/bootstrap-secret-drafts.sh rotate --context "$EXACT_CONTEXT" \
  --keyring-file "$NEXT_KEYRING_FILE" \
  --expected-revision "$EXPECTED_KEYRING_REVISION"
```

Key CLI требует абсолютный путь, parent directory `0700`, owner UID,
единственную hard link и regular file `0400/0600`; новый файл создаётся через
`O_EXCL`, сохраняется `0400` и fsync. Ротация сохраняет все прежние read keys,
добавляет один новый current key и выполняет Kubernetes resourceVersion CAS.
Повтор с теми же bytes подтверждает прежний результат. Lost reply требует
exact readback; скрипт не делает hidden retry замены. Успех rotate включает
подтверждение, что broker уже прочитал новый revision/digest и записал guard.

Лимит — 128 retained keys и `1 << 24` зарезервированных шифрований текущим
ключом. Резервация перед AES-GCM устойчива к restart, конфликтам реплик и
неизвестному результату API. Readiness сверяет тот же лимит без расходования
счётчика; на границе требуется следующий ключ. Автоматическое удаление старых
ключей не выполняется: сохранённые черновики продолжают расшифровываться.
Rollback на старый manifest закрыто отклоняется, поэтому откат приложения
не должен откатывать keyring или guard.

## Composition, readiness и диагностика

Все пять draft RPC проходят authority verifier в том же сервере, что и
рабочие операции. Семь CP work RPC используют общий client operation
profile с точными методами `platform.runtime-secret-drafts.*`. UID/RV, lease и generation
проверяются до эффекта и при exact readback. Public ответ содержит только
безопасные metadata, включая owner Secret version; версия draft после publish
совпадает с версией возвращённого Secret.

Startup проверяет CP work readiness, keyring/guard и Kubernetes API, затем
выполняет ограниченный recovery cycle. Recovery worker запускается после
барьера и входит в общий cancel/join. Неудачный последний recovery делает
readiness отрицательной; успешный цикл восстанавливает её. Recovery не
расшифровывает и не повторяет publish, а выполняет отдельные owner решения
KEEP/DELETE для encrypted и runtime materialization с точными preconditions.

Метрики `kodex_secret_broker_draft_recovery_cycles_total`,
`kodex_secret_broker_draft_cleanup_readbacks_total` и
`kodex_secret_broker_draft_recovery_ready` используют закрытые labels.
Успешная очистка учитывается и при последующей ошибке другого эффекта/ACK.
Логи и RPC errors не содержат исходную Kubernetes/CP диагностику, plaintext,
key material, grant или descriptor.

## Проверенная библиотечная документация

Context7 resolve-library-id для Go завершился `fetch failed`; использованы
официальные [Go cipher](https://pkg.go.dev/crypto/cipher#NewGCMWithRandomNonce)
и [Kubernetes Secret](https://kubernetes.io/docs/concepts/configuration/secret/).
Random nonce добавляет 28 bytes overhead; число шифрований одним ключом
ограничивается устойчивым счётчиком ниже границы 2^32. Immutable Secret и
UID/resourceVersion preconditions защищают сохранённый encrypted descriptor.

## Проверки и допуск

Публичная точка входа `make test-secret-broker-drafts` выполняет Go race/vet/build,
bootstrap/CAS/recovery fixtures и render обоих полных профилей. Composition test
соединяет настоящий RPC caster, owner adapter, AES-GCM, FileKeys, durable guard,
encrypted storage и runtime storage через fake Kubernetes/generated CP stub:
save без активного Secret, validate, publish exact runtime bytes, отдельный
save/discard и отказ/восстановление readiness. Он не подменяет защищённый live
CP/Kubernetes path. Отдельно проверяются Proto/codegen и authority policy/render.

Exact SHA и результаты каждого запуска фиксируются в PR #1068 после commit.
Live CP→broker, Kubernetes, browser, staging и production — NOT RUN.
Общий baseline, gate и приёмка принадлежат #1031.
