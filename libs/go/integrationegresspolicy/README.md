---
id: GO-LIB-INTEGRATION-EGRESS-POLICY-001
title: Общий контракт сетевого допуска OpenAPI-интеграций
type: library-readme
status: approved
owner: backend
version: 1.0.0
updated: 2026-09-24
---

# Сетевой допуск OpenAPI-интеграций

Control-plane формирует документ из разрешённого множества HTTPS origins и
полного проверенного DNS snapshot. Egress-gateway потребляет только immutable
документ точного поколения и digest. Профиль предназначен только для отдельного
listener интеграций; обычный CONNECT, STT и mail не получают эти destinations.

В документе нет учётных данных, содержимого OpenAPI или аргументов вызова.
Сохранение разрешения, транзакция владельца, Kubernetes publication, готовность
listener и telemetry принадлежат соответствующим сервисам. Пустой список
origins допустим при первом запуске, но не даёт сетевого доступа.

Изменение schema/digest требует совместного обновления producer и consumer.
Невалидный hostname, частный/mixed DNS snapshot, устаревший DNS, неизвестный
порт либо несовпавший digest закрыто отклоняются. Ошибки не содержат адресов.
