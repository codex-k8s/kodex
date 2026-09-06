---
id: HTTP-OWNER-GATE-1075
title: Согласованная HTTP-проекция интеграционного OwnerGate
type: verification
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Интеграционный OwnerGate

Refs #1075, #1045, #1046, #1022; MVP-UI-41/42.

Существующие ListOwnerGates/GetOwnerGate/ResolveOwnerGate передают ответ CP
через общий descriptor mapper. Источник actor/tenant и разрешение операции
остаются в прежнем authenticated/proof/owner пути; HTTP не назначает intent
по параметрам клиента. Resolve сохраняет существующие OCC/idempotency и
ответ владельца. Повторное чтение не инициирует внешний эффект.

При наличии IntegrationIntent требуются исходные connection/effect identity,
закрытый resource kind, digest, bounded scope и safe preview. Обычный gate
без intent не получает выдуманный integration context. HTTP не восстанавливает
пропущенные сведения из mutable integration catalog. Безопасное содержимое
preview формирует owner; gateway проверяет существующие границы OpenAPI,
не пытаясь классифицировать secret по названию поля.

DecisionConsequences содержит по одной записи на разрешённое решение.
Неизвестные/повторные решения и пропуски закрыто отклоняются. Обязательные
false executesExternalEffect/terminalForRun сохраняются, а не исчезают из-за
proto3 omission. Эти значения передаёт CP; HTTP не вычисляет эффект или
terminal semantics локально.

Owner intent data не проходят строковые enum/i18n преобразования. Source и
resolution AttachmentSet refs сохраняются отдельно. Malformed payload не
заменяется частичным gate: list, single и resolve возвращают безопасную
ошибку. Одиночные ответы дополнительно связаны с запрошенным gate ref.

Регрессии используют generated RPC fake и проверяют одинаковую проекцию всех
трёх HTTP путей, nil/ordinary intent, false flags, исходные literal values,
attachment lineage и malformed scope/digest/enum/consequences. Реальная
owner materialization проверяется отдельно в #1046; browser и внешний
integration effect требуют общей приёмки #1031.
