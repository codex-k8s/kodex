---
id: OPS-DOC-PERSONAL-TOKEN-ALLOWANCE-2033
title: Конечный допуск персонального TOKEN вызова
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-22
---

# Конечный допуск персонального TOKEN вызова

Исправление [#2033](https://github.com/codex-k8s/kodex/issues/2033) относится
к существующему personal execution unit, включая GitHub. Оно не меняет схему,
права service caller, broker, произвольные HTTP endpoints либо provider команды.

## Карта authority и потребителей

| Граница | Источник полномочий и результат |
| --- | --- |
| Намерение | `runs.initiated_by` точного root; immutable Connection/definition/configuration/credential pins. Actor из запроса не принимается. |
| Claim | `platform.runtime.integration-invocations.claim`, проверенный integration-gateway, прежний canonical runtime/grant/approval selector; `integration.manage` исходного actor вычисляется существующим access evaluator. |
| Dispatch envelope | В возвращаемом `WorkLease.expires_at` минимум owner lease и текущего ACL. DB lease, generation и fence сохраняют прежнюю семантику reconciliation. |
| Begin | `PersonalIntegrationCredentialService.BeginPersonalIntegrationInvocation`, `platform.personal-integration-calls.begin`; Connection→binding→invocation, exact pins и один immutable admission receipt. |
| Disclosure | `requireAccessUntil` заново разрешает root actor и текущие bindings; минимум этого allowance и DB lease проверяется до/после COMMIT и перед транспортным ответом. |
| Ответ и native consumer | Новый `authorized_until=5` — timestamp владельца. Gateway требует его presence/validity, проверяет бюджет и сохраняет минимум с original claim без перезаписи исходного claim. Original claim ceiling фиксируется monotonic deadline до Begin RPC; fresh bound может только сузить унаследованный context. Native request получает ограниченный context. |
| Audit и результат | Прежний `PERSONAL_INTEGRATION_CALL_ADMITTED`; completion использует прежние receipt, claimed workload, generation/fence и owner DB lease. Новое domain event не добавляется. |

`requireAccessUntil` повторно использует исходный allow-only evaluator. Среди
текущих выигравших альтернатив выбирается наиболее поздний `valid_until`;
активная бессрочная альтернатива не имеет ACL deadline. Будущие bindings не
продлевают текущее решение. Admission всегда ограничен конечным owner lease.

## Матрица жизненного цикла

| Переход | Закрытый результат |
| --- | --- |
| Claim ACK после ACL expiry | Envelope уже истёк; gateway не вызывает Begin/native adapter. |
| ACL сокращён между claim и Begin | Fresh Begin deadline сужает прежний envelope; недостаточный бюджет закрывает dispatch. |
| Begin COMMIT ACK после allowance | Receipt может быть durable; descriptor и новый dispatch не выдаются. Неизвестный COMMIT не повторяется. |
| Grant восстановлен до первого Begin | Это новая текущая проверка допуска; она не продлевает original claim envelope. |
| Grant восстановлен после consumed Begin | Повтор exact Begin не создаёт второго EXECUTE receipt и не выдаёт descriptor. |
| Native effect начат вовремя, ACK пришёл после dispatch allowance | Gateway сохраняет факт исполненного adapter. Прежний completion path принимает результат в пределах прежнего DB lease; срок DB lease не расширяется. |
| Исчерпан прежний DB lease | Сохраняются прежние UNKNOWN_OUTCOME и read-only recovery rules; исправление не разрешает поздний обход fence, generation или lease. |
| Credential rotate/revoke | Прежние exact binding/credential pins и сериализация; admitted result не превращается в новый вызов. |

## Совместимость и воспроизведение

Старый CP / новый gateway: отсутствие `authorized_until` закрывает personal
вызов до adapter. Новый CP / старый gateway: поле5 сохраняется как unknown;
прежняя strict проверка ответа закрывает personal вызов. Нужна совместная
поставка CP/generated contracts/gateway; shared calls сохраняют прежний путь.

Native regression входит в `make test-personal-integration-token-allowance-postgres`:
текущая canonical046 без изменения исторического034 upgrade-профиля,
исходный второй actor, реальный claim, native COMMIT с задержкой только ACK,
отсутствие descriptor, сохранённый одноразовый receipt, revoke/regrant и completion
уже допущенного результата. Gateway regression использует настоящий dispatch
helper и synthetic owner/adapter, включая старый wire descriptor. Это не
подтверждает live JWT, внешние GitHub permissions или действительные credentials.

Генерация: `buf generate --template buf.gen.local.yaml` с закреплёнными
`protoc-gen-go v1.36.11` и `protoc-gen-go-grpc 1.6.2`.

## Отдельный открытый аналог

Verification probe остаётся отдельным gap #2033: `currentPersonalCredentialTestInput`
использует `personal_credential__read_owned.sql` с boolean `integration.view`;
`BeginPersonalIntegrationCredentialProbe` не передаёт Until и не проверяет expiry
после успешного COMMIT ACK. Gateway `personal_credentials.go` после ACK запускает
bounded provider READ. Поэтому при истечении view allowance во время ACK новый
verification READ всё ещё возможен; claim также уже содержит descriptor metadata.
Это статический source finding, не отдельный native regression в этом срезе.
Существующие exact lease, pin, revoke и одноразовый PROBING предотвращают повтор
после lost ACK, но не сохраняют finite ACL deadline. Общая готовность personal
credentials этим исправлением не утверждается.
