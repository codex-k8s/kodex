---
id: OPS-WORKER-AUTHORITY-1059
title: Служебная авторизация EMAIL и Mattermost
type: operations
status: approved
owner: backend
version: 1.0.0
updated: 2026-09-05
---

# Границы

Источник: #1018, #1059, зависимые #1030/#1037/#1046. Используется существующий
internal-rpc-authority; нового сервиса, универсального grant либо сетевого
разрешения этот unit не вводит. EMAIL не получает права другого workload;
Mattermost остаётся optional в `web-with-mattermost`.

| Переход | Владелец и связывание | Результат / отказ |
| --- | --- | --- |
| Fresh install | Offline генератор выпускает индивидуальный ES256 key для точного workload | Private key доступен только его grant-agent, public key доставляется CP. Не является ротацией существующей установки. |
| Startup grant | Grant-agent принимает только закрытый workload registry; key ID связывает workload и generation | Подпись issuer/audience/SPIFFE/workload, TTL 4 минуты; atomic write и exact readback до readiness. Неизвестный workload или чужой key ID отклоняется. |
| Refresh / restart | Тот же workload и credential generation, свежие revision/JTI | Ротация grant не сбрасывает принадлежащий CP durable high-watermark; rollback отклоняет CP. |
| Authority proof | EMAIL: три platform.email операции; Mattermost: восемь platform.interactions операций из producer profile #1046 | mTLS + application grant + local issuer → CP resolver → signed context → exact RPC. Grant не назначает project, actor либо invocation из произвольного payload. |
| Issuer state | Индивидуальный LOGIN, NOINHERIT/NOBYPASSRLS, CONNECT и SET только issuer capability | Readback/restore и session_user binding используют существующую owner-модель. Доступ к publisher/verifier capability не выдаётся. |
| Доставка issuer keys | Publisher меняет только точные имена Secrets, назначенные target registry | Readback/restore credentials и possession keys относятся к тому же workload/role; wildcard Secret access не добавляется. |
| Отказ / revoke | Недоступный ключ, issuer, trust, generation, restore fence либо CP | Закрытый отказ; отсутствие mounts не считается готовностью. Повтор business effect не добавляется. |

Новых business events нет. Авторитетный read path: существующий publisher
target registry, readback/restore state, CP grant watermark и typed owner RPC.
CP SQL handlers, operation policy, worker trust configuration и watermark
constraint принадлежат #1046. EMAIL mounts и consumer принадлежат #1037;
Mattermost consumer принадлежит #1030.

# Установка

Изменение SQL относится к поддерживаемому fresh-install baseline authority.
Это не inplace upgrade живой БД. Disposable deployment выполняется только
после полной интеграции. Запуск генерации либо materialize-secrets против
живой установки этим unit не разрешается.

Новые key/material refs не являются секретными значениями. Тесты используют
временные каталоги и синтетические ключи, не печатают JWK/grant/DSN.
Private key не передаётся через public trust. Почтовые порты и live mail
egress остаются вне этого unit.

# Проверка

До handoff необходимы local grant issuance/rotation/readback и negative
workload/key tests, fresh key separation, issuer profile, SQL principal
проверки и install/render assertions. Защищённый сквозной consumer → CP путь
проверяется на совместном дереве #1030/#1037/#1046/#1059; unit tests не
подменяют эту проверку. Общий review выполняется на итоговом SHA по #1018.

Публичные точки входа:

- В `services/internal/internal-rpc-authority`: `go test -race ./... -count=1`,
  `go vet ./...`, `go build ./...`. Включены запуск offline key generator,
  индивидуальность 11 ключей, отсутствие private material в public trust,
  точные права файлов, signer rotation и отрицательные identity/key случаи.
- `make test-internal-rpc-authority-postgres`: одноразовая PostgreSQL,
  реальные LOGIN/CONNECT/SET issuer, запрет других authority capabilities.
- `make test-install-contract`: полный набор статических и динамических
  проекций обоих consumer и замкнутый реестр из 22 runtime principals.
- `make test-worker-authority-projections`: два итоговых профиля,
  target revision 7, точные семь publisher Secret permissions и входящий
  CP/readback/restore/PostgreSQL путь. Проверка не заявляет готовность самого
  EMAIL consumer до интеграции #1037/#1046.
- `make test-web-only-release` и `make test-internal-rpc-authority-abi-render`:
  существующие проверки release render и совместимости authority sidecars.

Проверена актуальная документация Goose через Context7 `/pressly/goose`:
`UpContext`, транзакционное применение SQL и директивы `StatementBegin`.
SQL остаётся частью единственного fresh-install baseline, не миграцией
существующей живой установки. Результаты локальных запусков привязываются
к точному SHA в PR по #1059; защищённый consumer → CP и live provider до
общей интеграции имеют статус `NOT RUN`.
