---
doc_id: REF-CK8S-0004
type: backlog-audit
title: "kodex — стартовый аудит backlog для программы рефакторинга"
status: active
owner_role: EM
created_at: 2026-04-21
updated_at: 2026-04-25
related_issues: [78, 281, 282, 294, 309, 322, 376, 380, 470, 488, 489, 582, 586, 599, 600, 601, 602]
related_prs: [587]
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-21-refactoring-wave0"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-21
---

# Стартовый аудит backlog для программы рефакторинга

## Цель
Зафиксировать минимальный начальный список открытых артефактов, которые уже влияют на новую программу, чтобы не продолжать старую модель по инерции.

## Активный PR

| Артефакт | Решение | Причина | Следующее действие |
|---|---|---|---|
| PR #587 | закрыт как заменённый справочный артефакт | PR реализовывал legacy-подход к Mission Control prototype и не является базой новой платформы | оставить как исторический справочный материал без продолжения |

## Ключевые open issues

| Issue | Домен | Решение | Причина | Следующее действие |
|---|---|---|---|---|
| #281 | Onboarding репозитория | переписать | сценарий остаётся релевантным, но должен жить в новой provider-first модели onboarding | держать как будущую задачу исполнения после доменов `Проекты и репозитории` + `Provider-native рабочие сущности` |
| #282 | Подключение существующего репозитория | переписать | сценарий остаётся релевантным, но должен жить в новой provider-first модели onboarding | держать как будущую задачу исполнения после доменов `Проекты и репозитории` + `Provider-native рабочие сущности` |
| #586 | Knowledge/memory | переписать и отложить | vendor-specific формулировка больше не подходит, но сам домен останется нужен позже | вернуться после домена `Документация и knowledge lifecycle` |

## Закрытые issues по результатам wave 5 и предыдущих checkpoint

| Issue | Домен | Решение | Причина | Зафиксировано в |
|---|---|---|---|---|
| #470 | Risk/release governance | закрыть как поглощённую | каноника risk/release governance, human gates, evidence contract и release safety-loop зафиксированы в wave 4 | `refactoring/14-risk-and-release-governance.md`, `refactoring/15-human-gates-and-evidence.md`, `refactoring/16-release-safety-observability-and-notifications.md` |
| #376 | Provider metadata usage | закрыть как поглощённую | provider-native поля, relationships, milestone/project fields, watermark и приёмка уже вошли в канонику wave 1-3 | `refactoring/06-product-model.md`, `refactoring/08-provider-native-work-model.md`, `refactoring/11-data-and-state-model.md`, `refactoring/12-provider-integration-model.md`, `refactoring/13-artifact-contract-and-acceptance.md` |
| #488 | Delivery governance | закрыть как поглощённую | compact PR policy и `prototype -> production conversion` уже вошли в канонику программы | `refactoring/01-program-charter.md`, `refactoring/05-delivery-and-risk-principles.md`, `refactoring/24-pre-wave7-documentation-rebuild-plan.md` |
| #489 | Legacy revise trigger | закрыть как устаревшую | старый label-driven `run:*:revise` trigger не является каноникой новой orchestration-модели | wave 1-3 каноника `agent-manager` / `provider-hub` / acceptance policy |
| #309 | First-run onboarding | закрыть как поглощённую | проблема `time to first successful run`, домашнего экрана, guided first-run path и универсальных workspaces вошла в канонику wave 5 | `refactoring/17-console-and-ux-model.md`, `refactoring/18-workspaces-onboarding-and-operator-surfaces.md`, `refactoring/19-flow-role-prompt-and-settings-ux.md` |

## Дополнительные legacy-кандидаты
- Открытые issues из старых спринтов и stage-пакетов, построенные вокруг старой control-plane-центричной модели, считаются legacy-candidate backlog.
- Их детальная ревизия выполняется перед каждым следующим доменным срезом, а не откладывается до конца программы.

## Обязательный checkpoint по GitHub backlog
Отдельная большая ревизия GitHub Issues/PR должна пройти не "когда-нибудь потом", а в фиксированный момент программы:
1. после принятия волны 2 по целевой архитектуре и service boundaries;
2. после принятия волны 3 по данным и provider integration;
3. до запуска первых больших implementation waves.

На этом checkpoint нужно:
- закрыть obsolete issues, которые завязаны на старую control-plane-центричную модель;
- переписать или пересоздать issues, которые остаются релевантными, но меняют смысл в новой архитектуре;
- проверить старые открытые PR на предмет `закрыть / заменить / переписать позже`;
- обновить этот файл по фактическому состоянию GitHub backlog.

### Результат checkpoint после принятия wave 3
Отдельный компактный GitHub backlog pass выполнен.

Зафиксированы такие действия:
- `#376` закрыт как `absorbed`;
- `#488` закрыт как `absorbed`;
- `#489` закрыт как `obsolete`;
- `#470`, `#281`, `#282`, `#309`, `#586` переписаны под новую программу без legacy-framing.

Следующий обязательный backlog checkpoint выполняется после wave 6 и до старта первых больших implementation waves.

## Результат checkpoint после принятия wave 6
Checkpoint между wave 6 и wave 7 выполнен и зафиксирован отдельным документом:
- `refactoring/23-backlog-checkpoint-before-wave7.md`

На этом checkpoint:
- сформирована первая кодовая очередь wave 7: `#599`, `#600`, `#601`, `#602`;
- legacy-issues старых sprint execution chains переведены в состояния `закрыть как устаревшие / заменить`;
- часть ранее открытых задач перепривязана к waves `8`, `11`, `12`, `13`, `14`, `15`, `17`;
- отдельно подтверждено, что открытых PR, требующих backlog-alignment, на момент checkpoint нет.

Следующий обязательный backlog pass теперь имеет смысл только после фактического результата wave 7, а не вместо начала кода.

## Ритуал обновления этого файла
Перед стартом каждой новой волны:
1. перечитать актуальные open issues и active PR;
2. обновить таблицы `решение / причина / следующее действие`;
3. отдельно отметить, какие artifacts:
   - закрываются как obsolete;
   - переписываются под новый домен;
   - остаются reference-only;
   - становятся execution issues ближайшей волны.
