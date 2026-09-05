---
id: OPS-CATALOG-TOTALS-1046
title: Авторитетные страницы Runs и Artifacts
type: operational-guide
status: approved
owner: control-plane
version: 1.0.0
updated: 2026-09-05
---

# Авторитетные страницы Runs и Artifacts

Источник: Issue #1046, Epic #1018, MVP-UI-05 и Home. Actor приходит из
проверенного transport principal; идентификатор проекта в фильтре полномочий
не создаёт.

| Сценарий | HTTP / RPC | Owner и результат | Потребитель |
| --- | --- | --- | --- |
| Runs Home | GET runs → ListRuns | PostgreSQL, run.view, runs/page/total | Home и список Runs |
| Общие файлы | GET artifacts → ListArtifacts с пустым projectRef | PostgreSQL, artifact.view, artifacts/page/total | Общий каталог файлов |
| Файлы проекта | GET projects/{projectRef}/artifacts → ListArtifacts | Тот же artifact.view и дополнительный фильтр проекта | Каталог проекта |

HTTP-потребление общего маршрута и total добавляется в существующем unit #1045;
наличие этого owner-контракта само по себе не доказывает готовность интерфейса.

Каждый ответ читает total до pagination и строки страницы в одной read-only
RepeatableRead транзакции. SQL eligibility и проверка действий используют
один transaction_timestamp. Count применяет те же tenant, actor, authority
project, пользовательские фильтры и canonical visibility rule, что и строки.
Дополнительная независимая project.view проверка артефактов не вводится.

Query — буквальная подстрока без wildcard semantics, максимум 200 символов.
Пустой projectRef охватывает все доступные проекты и доступные личные файлы;
это не только project-null строки и не browser fanout. Artifacts по умолчанию
использует lifecycle ACTIVE; тип, scan state и source фильтруются до count/limit.
Run states — закрытое множество уникальных состояний; порядок states
канонизируется. Неизвестные и UNSPECIFIED значения отклоняются.

Порядок страниц остаётся ref ASC. Cursor связывает organization, actor,
authority project, вид и полный нормализованный фильтр. Смена фильтра или actor
возвращает InvalidArgument. Total отражает снимок конкретного запроса:
между страницами состояние и права могут измениться. Cursor не обещает
неизменяемый снимок всего многостраничного просмотра.

Чтения не меняют состояние, не используют OCC/idempotency и не публикуют
события. Авторитетный read path — повторный List; скрытые строки не входят
ни в страницу, ни в total. Ошибки SQL закрываются Unavailable.

Проверка: Bootstrap component scenarios OIDC membership и direct run
проверяют ограниченного читателя, скрытый проект, чужой tenant, filtered count,
literal query, actor/filter cursor, три страницы файлов двух проектов и
личной области. Staging и browser acceptance выполняются отдельно.

Вклад `db2592c51c4500d7b520860a4272bddb50881fe2` перенесён поверх
Env638/Interaction637. Авторский targeted PostgreSQL — PASS (2.015 s).
На объединённом дереве repository/transport race, полный control-plane
vet/build, SQL boundary, Proto lint/build/replay, policy63 и authority ABI
render — PASS. Первый полный Bootstrap после переноса — FAIL (20.023 s):
общий прежний 20 s context исчерпан после успешных Home/Environment/managed
сценариев. Остальные ошибки были следствием отменённого общего context.
Бюджет расширенной последовательной тестовой матрицы увеличен до 60 s;
production deadline и локальные ограничения отдельных сценариев не меняются.
Повтор полного Bootstrap после изменения бюджета пока NOT RUN.
