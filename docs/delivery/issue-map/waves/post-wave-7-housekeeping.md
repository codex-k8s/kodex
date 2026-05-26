---
doc_id: MAP-CK8S-POST-WAVE-7-HOUSEKEEPING
type: issue-map
title: kodex — карта Issue технического среза после волны 7
status: completed
owner_role: KM
created_at: 2026-05-05
updated_at: 2026-05-26
---

# Карта Issue — технический срез после волны 7

## TL;DR

Карта фиксирует технический срез между волной 7 и следующими доменными волнами.

## Матрица

| Issue/PR | Документы | Домен | Статус | Примечание |
|---|---|---|---|---|
| #626 | `docs/delivery/audits/post-wave-7-n-plus-one-audit.md`, `docs/design-guidelines/go/services_design_requirements.md`, `docs/design-guidelines/go/check_list.md` | сквозной | закрыта как выполненная | Активный Go-код вне `deprecated/**` проверен на N+1 обращения. Очевидных блокирующих мест не найдено; граф членства принят как малый срез после кэша запроса и пакетного фильтра по статусам. |
| #803 | `Makefile`, `scripts/test-go-postgres.sh`, `scripts/test-go-postgres-k8s.sh`, `docs/design-guidelines/go/check_list.md`, `docs/design-guidelines/common/check_list.md`, `docs/design-guidelines/go/infrastructure_integration_requirements.md` | сквозной | закрыта как выполненная | Go unit/component контур отделён от PostgreSQL integration tests; явный integration target получил Kubernetes-native runner без обязательного Docker daemon и Docker fallback только для локальной разработки. |
