---
id: OPS-CODE-QUALITY-1823
title: Подготовка кода к построчному ревью
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-21
---

# Подготовка кода к построчному ревью

Задача: [#1823](https://github.com/codex-k8s/kodex/issues/1823).
Исходный main: `66c1a8ddafd3bb03d80ad6ff09b57759e582dfb5`.
Работа выполняется одним PR. После приёмки этой задачи владелец принимает
решение о слиянии, затем выполняет построчное ревью всего кода. Этот PR
самостоятельно не сливается и не является полным review/приёмкой прототипа.

## Границы и решения владельца

- По поручению21.10 один общий рефакторинг охватывает библиотеки, сервисы,
  шлюзы, PWA и инструменты; отдельные PR по слоям не создаются.
- Go-комментарии: английское пояснение, непосредственно следом русский
  перевод; смысл, ограничения и эффекты описываются по прочитанному коду.
  Это явное исключение из прежнего русского-only правила комментариев.
- Всё generated исключено из поиска и требования комментариев. Шаблоны
  генерации не меняются ради этой задачи. Handwritten генераторы остаются
  собственным кодом и проверяются, их output — нет.
- После изменения проверяются сборка и unit-тесты затронутых модулей.
  Полная приёмка, deploy, browser QA, реальные ИИ-запуски и component/live
  проверки не выполняются; их отсутствие не называется PASS.
- Поведение, authority, порядок проверок, wire и транзакции не изменяются.
  Применённые миграции неизменяемы. Похожая форма не доказывает общий смысл.

## Воспроизводимый анализ

`tools/code-quality/package-lock.json` закрепляет `jscpd@5.4.2` и нативный
пакет платформы. Документация проверена через Context7 `/kucherenko/jscpd`;
точные параметры подтверждены установленным `jscpd --help`.

```bash
npm ci --ignore-scripts --no-audit --no-fund --prefix tools/code-quality
node --test tools/code-quality/*.test.mjs
node tools/code-quality/scan-duplicates.mjs /absolute/private/report-directory
node tools/code-quality/summarize-report.mjs /absolute/private/report-directory
go test ./tools/code-quality/go-docs
go run ./tools/code-quality/go-docs -root . \
  -inventory /absolute/private/report-directory/source-inventory.json \
  -output /absolute/private/report-directory/go-docs.json -check
```

Go запускается с `GOENV=off GOWORK=off GOTOOLCHAIN=go1.26.6`.
Для реестра без дубликатного поиска есть `--inventory-only` третьим аргументом
`scan-duplicates.mjs`; для измерения неполного покрытия опускается `-check`.
Каждый отчёт содержит все непокрытые объявления, а не только удачные примеры.
Проверка языков машинная, точность и подробность текста проверяются чтением.

Охват — tracked и новые собственные source-файлы, включая тесты, SQL,
конфигурации и скрипты; исключения generated определяются устойчивым путём,
именем output/lockfile или ведущим generated заголовком до начала кода.
Исключения не выбираются по случайному слову в test string. Порог фиксирован:
100tokens/10lines, comments игнорируются, переименование идентификаторов
учитывается согласованно. Никаких embeddings/внешней отправки кода нет.

## Исходные результаты и оставшаяся работа

- [x] Созданы отдельные Issue/worktree/ветка и один контур измерения.
- [x] Baseline исходного main:6535 собственных файлов,179 generated/lockfile
  исключений; анализатор обработал5717 файлов выше собственных порогов,
  2138 clone groups. Полный нормализованный реестр без тел фрагментов —
  `code-quality-1823-baseline.gen.json`, SHA исходного raw report
  `704072f12077a73dfde1d4b9451f14de2232e9606b042f2083015d09418ed339`.
  Из них928 Go,259 TypeScript; остальные — scripts/SQL/configuration/markup.
- [x] Исходный Go-реестр:2414 файлов,20901 объявление,0 с полным новым
  двуязычным форматом. Это не отсутствие существующих русских комментариев.
- [ ] Разобрать все группы: вынос либо адресное обоснованное исключение.
- [ ] Завершить подробные двуязычные комментарии и получить полный `-check`.
- [ ] Повторить анализ и build/unit всех затронутых модулей на конечном SHA.
- [ ] Передать единственный готовый PR владельцу; не объявлять частичную
  волну готовым результатом.

Первый вынос: `provider_diagnostic.go` / `provider_process.go` в runtimecontract
используют общий package-private `decodeCanonicalProviderObservation`.
Сохранены4096bytes/depth2, duplicate-key guard, exact keys, EOF, canonical
порядок, отдельные semantic validators и sentinels. Получатель заменяется
только после успеха. Это чистый wire helper, он не выдаёт authority и не
превращает diagnostic observation в business receipt. Added regression
проверяет сохранение обоих получателей при невалидном JSON.

Двуязычно описаны эти два контракта и их тестовые функции. Общий Go build/unit
runtimecontract — PASS LOCAL; точный SHA привязывается в квитанции коммита/PR.
Полностью описан собственный код и тесты модуля httpserver, его build/unit —
PASS LOCAL. Поведение этого модуля не менялось, добавлены только комментарии.
Unit нового inventory/Go checker — PASS LOCAL. Полный контур остальной
задачи пока NOT RUN; цель остаётся незавершённой.
