---
id: OPS-DOC-1046-CATALOG-INTEGRATION
title: Интеграция каталога моделей и owner SourceWork
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Состав объединённого checkpoint

В основной #1046 перенесён bounded CP contribution
`82d8e19539ec214ca337b23f76f15d0d682a942e` и исправления fixtures
`e428519462e9dd5c3ec2aaa4e81564aa312398d3`. Сохранены SourceWork/RoleImage
`dfd0621c52d982459d218973d37fac9ab424a716`, policy62, D4/D5/D6 и их
transport/authority paths. Дополнительные RPC не заменяют прежние declarations;
Proto объединён по исходникам и regenerated каноническим codegen.

Общие зависимости: exact `libs/go/runtimecontract` из e428, canonical v6
context snapshot из ранее принятого prerequisite и новый v7, registry v7.
Это общий source snapshot; handwritten controller/runner и их deploy diff
сюда не копируются. Profile revision 2 в environment renders интегрируется
в #1031 из соответствующих unit checkpoints.

Реальный owner путь включает bounded refresh task → signed broker observation →
durable account snapshot → exact publication candidate pins → Session affinity →
fresh RuntimeRevision с server reasoning mode/effort. Default берётся из
account model snapshot, override остаётся в canonical TOML. Встроенный список
моделей не заменяет отсутствующее remote observation.

System assistant с project NULL разрешается отдельным owner rule по точной
organization и `organization.manage`; обычная Agent ACL не расширяется.
Schema/diagnostic readback принадлежит фактическому runtimecontract validator.

На объединённом рабочем дереве PASS: targeted CP owner/domain/transport/app/
worker/client race, полный CP vet/build, shared runtimecontract race, SQL
boundary, Proto lint/build/codegen replay, policy codegen и authority ABI render.
До объединения source lifecycle PG PASS 2.367s на dfd tree; contribution full
Bootstrap PG PASS 18.425s на e428 tree. Полный PG объединённого дерева после
устранения загрязнения mailbox fixture: PASS 19.803s. Предыдущий полный запуск
FAIL 18.422s показывал legacy example mailbox без owner credential rows;
forward-only cleanup теста сохраняет fail-closed проверку настоящего Bind.
Live provider, deployment и общий triple review остаются NOT RUN.

Source UNCHANGED повторно проверяет типизированную совместимость документа с
текущим compiled package/catalog; совпадение старого content digest само по себе
не выдаёт READY после изменения поддерживаемого executable contract.
