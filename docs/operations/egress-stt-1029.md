---
id: OPS-MVP-EGRESS-1029
title: Сетевой профиль STT
type: runbook
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-05
---

# Сетевой профиль STT

Источники: #1018, #1029, MVP-UI-55..60 и GUIDE-DOC-003.

| Сценарий | Инициатор и полномочия | Путь и владелец | Результат и жизненный цикл |
| --- | --- | --- | --- |
| Распознавание | Пользователь с проверенным правом диктовки; control-plane разрешает активную STT-конфигурацию и отдельный credential binding | HTTP gateway → защищённый `SpeechToTextService.Transcribe` → stt-tts-service → CONNECT `egress-gateway:8081` → `api.openai.com:443` | STT владеет выполнением, TLS/CA и лимитами; настройки принадлежат control-plane. Egress не получает credential и не завершает TLS |
| Сетевой допуск | Сервером выбранный listener `8081` и точные namespace/pod selectors допускают только `stt-tts-service` | Immutable профиль `openai-stt`, workload `stt-tts-service`, operation `openai.transcription`; exact CONNECT/SNI → проверенный DNS snapshot → literal dial | Неизвестные host/port/profile отклоняются до внешнего dial; заголовки caller не назначают workload или operation |
| Readiness | Тот же STT workload и listener | `GET /readyz` возвращает revision/digest/profile/workload/operation фактически загруженной policy; STT затем проверяет provider через тот же CONNECT | `204` только при готовности, иначе `503`; readiness не выполняет распознавание, не создаёт event/receipt и не меняет состояние |
| Drain и отказ | Lifecycle процесса и resolver | Общий readiness закрывает новые CONNECT; оба listener закрываются, активные соединения отменяются и join ограничен общим бюджетом | Нет фоновых tasks, grants, retries и доменных событий egress; авторитетный read path `/policy` и `/readyz` |

Старый порт `8080` не расширяется для STT. Два listener используют общий
лимит соединений, заданный policy; per-source лимит применяется внутри
listener. Сетевой профиль не доказывает HTTP method внутри TLS: точный
`/v1/audio/transcriptions`, отсутствие redirect и допустимые параметры
обеспечивает адаптер STT. Неплатный provider readiness может читать каталог
моделей тем же адаптером. Назначение Kubernetes labels остаётся полномочием
оператора deploy, обычный пользователь не может создавать такие Pod.

Почтовые порты и STARTTLS этим изменением не открываются: отдельный email-bridge
реализуется в #1037, а его сетевой допуск остаётся отдельным решением владельца.
Поставка STT-профиля не закрывает почтовый критерий #1029 и не разрешает
email-bridge прямой либо wildcard egress.

## Проверка

`go test -count=1 -race ./...` из `services/external/egress-gateway` проверяет
parser, профиль, DNS/SNI, readiness, cancel/join и машинную policy.
`tools/verify-egress-gateway.sh` проверяет итоговый render без live-доступа. Сквозной
STT provider вызов и отрицательный сетевой сценарий выполняются на стенде
только после полной интеграции и привязываются к точному SHA.
