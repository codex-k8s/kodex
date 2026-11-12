---
id: OPS-DOC-2108-WORKER-INSTANCES
title: Гранты отдельных экземпляров в свежей установке
type: operations
status: approved
owner: architect
version: 1.0.0
updated: 2026-11-12
---

# Гранты отдельных экземпляров в свежей установке

Связано с #2108 и исходным механизмом #1220. Несколько реплик worker с v1
соревнуются за один workload revision watermark. Более свежий credential
одной реплики закрывает допуск другой, несмотря на её ещё действующий срок;
для сборщика это отменяет BuildKit, а owner затем фиксирует lease expiry.

`web-only` и `web-with-mattermost` подключают существующий компонент
`platform-worker-grant-instances`. Единственное изменение issuance — Pod UID
из Downward API и format marker. У runtime-controller переменная добавляется
существующему native init sidecar, без второго контейнера. Одноразовые admission
Jobs сохраняют отдельный штатный жизненный цикл.

| Переход | Владелец и проверяемая граница | Результат и чтение |
| --- | --- | --- |
| Fresh install | Schema включает instance watermarks, все CP readers поддерживают v2, authority image содержит issuer v2 | Новый Pod получает собственный UID; exact render/readback до запуска работы |
| Claim | mTLS peer, signed workload/generation/instance, время и owner registration | Owner назначает attempt/fence/lease; UID сам по себе не даёт полномочий |
| Renew/report | Тот же instance, свежий credential, exact attempt/fence/version | Owner продлевает lease; другая реплика не продвигает его revision watermark |
| Rotation | Общий generation floor и отдельный instance revision | Старый revision этого instance отклоняется; соседний остаётся независимым |
| Complete/cancel | Прежние owner predicates и terminal transaction | Граф, receipt и отзыв lease сохраняют существующий контракт |
| Expiry/dead-letter | Owner закрывает прежнюю попытку | История не переписывается; явный retry создаёт новую попытку |
| Pod replacement | Новый server-issued UID, свежий grant | Старый lease не переносится по имени Pod; прежний watermark остаётся в БД |
| Disable/revoke | Enabled registration, generation floor, expiry | Instance не обходит отзыв и не расширяет набор методов |

Это изменение конфигурации не создаёт нового события домена. Авторитетные
read paths — owner build status и durable grant watermarks; Kubernetes readiness
не заменяет их. Обычная ротация не является ротацией signer key.

Обновление существующей установки: сначала schema и все CP consumers, затем
authority image, затем env worker. Перед rollout подтверждается отсутствие
активной работы; при отказе старые receipts и terminal attempts сохраняются.
Возврат v1 при нескольких репликах вновь создаёт конфликт, поэтому восстановление
выполняется вперёд без снижения watermark и удаления истории.

Проверки: `make test-internal-rpc-authority-abi-render`,
`make test-web-only-release`, disposable PostgreSQL
`KODEX_CONTROL_PLANE_TEST_FILTER=TestWorkerGrantInstancesComponent make test-control-plane-postgres`.
Результаты конкретного SHA и установленной сборки фиксируются отдельно.

Проверены официальные Kubernetes docs через Context7: [Downward API](https://kubernetes.io/docs/concepts/workloads/pods/downward-api/),
`fieldRef.metadata.uid` обозначает уникальный UID Pod; пользовательский payload
или имя Pod его не заменяет.
