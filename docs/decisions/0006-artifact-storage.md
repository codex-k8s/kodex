---
id: ADR-MC-006
title: Bounded artifact storage boundary
type: decision
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-22
---

# ADR-MC-006. Bounded artifact storage boundary

## Решение

Control-plane владеет metadata и lifecycle Artifact. Fresh web-only профиль
первой версии хранит bounded content в PostgreSQL в той же tenant boundary,
чтобы upload, generated result и download работали без обязательного внешнего
object storage. Размер каждого объекта и суммарная транзакция строго ограничены.

Доступ к content возможен только через специализированные streaming RPC/HTTP
operations с owner eligibility, scan state и one-time download grant. Browser,
runtime Pod и optional adapter не получают PostgreSQL DSN или storage locator.

Object storage может быть добавлен как внутренняя реализация artifact content
port после MVP. Это не меняет `Artifact`, API, grants, provenance или lifecycle и
не превращает S3 connection в пользовательскую IntegrationDefinition.

Optional Mattermost/result mirror содержит только отдельную доставленную копию
или ограниченную ссылку и никогда не является источником истины.

## Последствия

- web-only installation имеет одну меньшую обязательную infrastructure
  dependency;
- large-object/multipart и отдельный object store backend относятся к POST-MVP;
- PostgreSQL backup включает metadata и bounded content согласованно;
- runtime and browser traffic остаются bounded streaming и не передают files по
  WebSocket/domain event.
