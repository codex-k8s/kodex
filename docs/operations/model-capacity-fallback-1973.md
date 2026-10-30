---
id: OPS-DOC-MODEL-CAPACITY-1973
title: Проверенная перегрузка модели и границы резервного исполнения
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-30
---

# Перегрузка и резервная модель

Задача [#1973](https://github.com/codex-k8s/kodex/issues/1973), эпик
[#1830](https://github.com/codex-k8s/kodex/issues/1830), общий Draft PR
[#1970](https://github.com/codex-k8s/kodex/pull/1970).

`gpt-6.1-terra` и `medium` уже присутствуют в свежем каталоге подключённого
Codex account. Отдельный ограниченный запуск на staging подтвердил модель и
reasoning в фактическом RuntimeRevision, шесть тестов сотрудника и шесть
скачанных CLEAN-файлов с совпадающими хешами. Это проверка доступности Terra,
а не воспроизведение перегрузки OpenAI или автоматической замены модели.

**Настройка списка замен и автоматическое продолжение пока не реализованы.**
Причины последних сбоев PDLC — delivery limit и стоимость owner completion;
они не являются доказательством перегрузки. Основная команда остаётся Sol/high.

## Передача проверенной причины

Ранее parser распознавал структурированный `serverOverloaded`, а terminal
presentation уже формировал `i18n:PROVIDER_OVERLOADED`. При completion runner
сводил причину к общему `PROVIDER_UNAVAILABLE`, и control-plane терял отдельную
классификацию. Подготовленный дополнительный код `PROVIDER_OVERLOADED`
сохраняет точную причину; он не разрешает повтор и не меняет RuntimeRevision.

| Участник и граница          | Источник полномочий и проверка                                                                                                             | Результат                                                                                                        |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| Codex parser → broker       | Закрытый structured terminal code, FAILED, принадлежащий provider процесс; private frame с точными RR/input/execution/session/turn/attempt | Диагностика, которую нельзя присвоить через публичный Result JSON                                                |
| Runner → callback           | `Result.ProviderDiagnostic(input)`, прежние измерения usage и archive pins; только согласованный terminal overload получает новый код      | Старые generic completions остаются совместимыми; недоказанный overload остаётся generic unavailable             |
| Callback → owner RPC        | Bearer и execution headers; Validate и Matches по сохранённому immutable input до RPC                                                      | Missing/false/mismatched overload получает400 без owner command и cleanup                                        |
| Owner → PostgreSQL          | Прежние service identity, exact lease/fence/generation, root lock, command idempotency и закрытый safe-error enum                          | Единственная failed completion с usage, summary, durable event и receipt; точный replay не создаёт новую попытку |
| PostgreSQL → graph/event/UI | Авторитетные существующие защищённые read paths; новый token в русском и английском реестре сообщений                                      | Владелец видит отдельную причину без исходного текста ошибки или секретов                                        |

Новый код допускается только из доверенной completion boundary. Он не является
независимым правом caller и не заменяет авторизацию RPC. Прежний completion с
общим кодом и корректной диагностикой продолжает читаться при rolling upgrade;
текст `server_overloaded` без проверенной диагностики не становится сигналом.
Схема, Proto/OpenAPI/AsyncAPI wire shapes и SQL predicates не меняются.

## Матрица жизненного цикла

| Состояние или событие                              | Действующий переход                                          | Будущая policy не должна                                                         |
| -------------------------------------------------- | ------------------------------------------------------------ | -------------------------------------------------------------------------------- |
| Подтверждённый terminal overload                   | Прежний failed completion, lease закрыт; причина сохранена   | Менять уже закреплённый model/effort в старом RR                                 |
| Quota/auth/timeout/unavailable/invalid output      | Прежний отдельный failure path                               | Подменять причину перегрузкой или менять account/credential                      |
| Completion replay/lost ACK                         | Прежний ключ и payload возвращают прежний receipt            | Создавать второй successor либо второй внешний эффект                            |
| Cancel/delete/revoke/expiry                        | Прежняя terminal/authority boundary отклоняет позднюю работу | Оживлять закрытый root, grant или lease                                          |
| UNKNOWN внешнего WRITE                             | Прежний reconciliation/blocked path                          | Повторять действие с новым idempotency key                                       |
| Известный SUCCESS внешнего WRITE                   | Прежний защищённый read-only rejoin                          | Публиковать действие повторно при смене модели                                   |
| Неподтверждённая остановка writer/legacy workspace | Автоматическая замена отсутствует                            | Объявлять восстановление успешным либо запускать второй writer                   |
| Будущий published fallback                         | Пока NOT IMPLEMENTED                                         | Увеличивать2 concurrent executions, deadlines, attempts, capture/account budgets |

Для полного #1973 нужны versioned owner UI/API policy с ordered model/effort,
fresh same-account catalog validation, exact successor provenance, durable
workspace/writer stop join, история выбора и проверки racing completion/cancel.
Эти возможности нельзя объявлять работающими по появлению отдельного error code.

## Проверка и возврат

Локальные регрессии покрывают broker codec и private provenance, closed overload
classification, JSON completion guards, callback RPC/cleanup boundary, native
owner persistence/replay/event/graph/no-successor и оба перевода UI.
Изолированная перегрузка в тестах — локальная оснастка, не реальный ответ OpenAI.
Фактически выполненные проверки и обслуживаемые SHA учитываются отдельно в PR.

Проверены Context7 `/openai/codex`: structured turn errors и различие с
`usageLimitExceeded`; официальные
[ошибки Agents API](https://developers.openai.com/api/docs/guides/agents-api/errors)
и [Terra](https://developers.openai.com/api/docs/models/gpt-6.1-terra).
Строковый HTTP API код не подставляется в другой протокол без его parser.

Возврат использует прежние совместимые immutable images штатным scoped release.
Сохранённые error codes не переписываются; сначала reader/UI принимают новый
код, затем producer начинает его отправлять. Старый reader, который закрыто
отклоняет новый код, не должен обслуживать нового producer. До полного
consumer-first rollout положительная staging-передача нового кода — NOT RUN.
Production, общие auth/ingress/firewall и бюджеты не изменяются; секреты не
публикуются.
