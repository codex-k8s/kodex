---
id: OPS-DOC-1274
title: Проверка PWA в Chromium, Firefox и WebKit
type: verification
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-08
---

# Browser-проверка #1274

Источники: #1031, #1274, OPS-DOC-1240, OPS-DOC-1243 и FE-DOC-001.
База разработки — `0d088bf693ec906d418596a30d22250d0b747030`.
Проверяются реальные PWA-компоненты с синтетическими DTO/ответами на локальном
preview. Ни один результат этого документа не является staging, vendor,
hardware microphone или полным MVP-UI-01..61/CFG-01..03 PASS. Финальный
проверенный head и точные artifact digests фиксируются в PR.

## Публичный профиль

Из `services/staff/control-center`:

```sh
npm ci
npx playwright install chromium firefox webkit
npm run build:synthetic
npx playwright test --config playwright.synthetic.config.ts --project=chromium
npx playwright test --config playwright.synthetic.config.ts --project=firefox
npx playwright test --config playwright.synthetic.config.ts --project=webkit
```

Без `--project` запускаются все три проекта. `--workers 3` позволяет ограничить
параллельный локальный пакет тремя workers. Профиль сохраняет `retries: 0`,
отключённые service workers и trace, строгий loopback port 43122 и запрет
переиспользования чужого preview. Только `vite.synthetic.config.ts` разрешает
Host `kodex.test`; production config не меняется. Chromium fake-media flags
не передаются Firefox/WebKit.
Длинный Home охватывает более двадцати экранов и имеет общий бюджет 120 секунд;
отдельные expect/API budgets не увеличены. Исторический timeout 75 секунд
остаётся FAIL, функциональный успех не означает performance PASS.

## Что означает проверка голоса

| Движок на Linux        | Источник звука                               | Recorder                                      | Граница результата                                                                         |
| ---------------------- | -------------------------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Chromium 149.0.7827.55 | Chromium fake device                         | Нативный MediaRecorder                        | Native recorder lifecycle с искусственным звуком; не hardware                              |
| Firefox 151.0          | WebAudio oscillator → MediaStreamDestination | Нативный MediaRecorder                        | Native recorder lifecycle с искусственным звуком; browser permission dialog не проверяется |
| WebKit 26.5            | Пустой synthetic MediaStream без устройства  | Явная e2e recorder-модель из project metadata | Только UI insertion/undo/cleanup; Blob модели не является закодированным аудио             |

В WebKit synthetic getUserMedia закреплён на `MediaDevices.prototype`: движок
может пересоздавать wrapper `navigator.mediaDevices`, теряя подмену отдельного
экземпляра между двумя capture. Повторные записи проверяются существующими
сценариями двух полей и последовательных действий.

У закреплённого Playwright 1.61.0 `grantPermissions(["microphone"])` для
Firefox/WebKit в существующем page закрыто отклоняется. Оснастка не обращается
к устройству и не добавляет production fallback. WebKit Linux не предоставляет
глобальный MediaRecorder: отдельный `voice-capability.synthetic.spec.ts`
без shim проверяет эту границу, понятную ошибку и нулевое число запросов.
Native WebKit capture/codec и hardware остаются **UNSUPPORTED/NOT RUN**.

`voice-recorder`/`native-recorder` annotations отличают модель от нативной
записи в отчёте каждого сценария. Поддержка конкретной Safari/ОС не выводится
из результата Playwright WebKit Linux. Реальные STT POST, девять серверных
containers, provider billing/credential и protected HTTP — отдельная приёмка.

## Матрица локального покрытия

| Требования / варианты                                             | Проверка                                                                                                                         |
| ----------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| MVP-UI-01/03/09/13/22/27; ru/en, 390/768/1280/1440/1920/2560/2900 | `ui-proof`: selector pagination/Escape/focus, assistant history, inspector, геометрия и отсутствие page overflow                 |
| MVP-UI-14/20/55–60; те же семь ширин                              | Voice selection и одна undo transaction в textarea/CodeMirror; adjacent typing, focus                                            |
| Voice lifecycle                                                   | Late response после route/unmount/availability/pagehide; ожидание начатого transcribe до прерывания именно позднего ответа       |
| Voice дополнительные варианты                                     | Scroll, managed lock, disabled/sensitive fields, отсутствие автоматического retry, synthetic429, cancel и один активный capture  |
| MVP-UI-10/28/32                                                   | Карточки проектов, сотрудников и процессов, server-owned synthetic aggregates/actions на 390/2900                                |
| CFG / окружения / secrets / automations                           | Существующие fixtures Git recovery/writeback/impact/rotation, runtime diff, scheduler preview, source permission и late response |
| Session/provider/email                                            | Существующие browser-state, provider lifecycle, mailbox и gate navigation fixtures; не реальные внешние effects                  |

Из giant Home-сценария отдельно исправлены устаревшие provider polling,
Kanban states и ожидание остановки автопагинации каталога. Доказательство
HTTP 412/503/504 использует точные response status/path, не наличие
зависящего от Chromium console text. Неожиданные ошибки сохраняются как FAIL.
Закрытый native Firefox scroll-linked advisory помечается отдельно, не
выдаётся за application error или доказательство приемлемой производительности.

Исправления оснастки связаны с #1277 (microphone), #1281 (preview Host),
#1282 (provider poll), #1283 (late response phase), #1284 (Kanban states),
#1285 (console diagnostics) и #1286 (catalog autoload). Ни один из этих
fixture failures не объявляется доказанным живым дефектом приложения.

Подтверждённая отмена запроса требует закрытого browser code и наблюдения
конкретного поколения: явного перехода сценария, закрытия/404 инспектора либо
AbortSignal исходного fetch. Наблюдатель передаёт input/init и Promise без
подмены. Binding может прийти после network event, поэтому незавершённая
диагностика сохраняется до итоговой проверки; неоднозначные запросы одного URL
не получают разрешение автоматически. Неизвестный код или отсутствие факта
отмены остаётся FAIL. Native font-preload/Firefox scroll advisory сохраняются
annotations; их предупреждения о производительности не считаются устранёнными.

## Прикладные исправления

- #1290: два canonical pattern IntegrationDefinition экранируют literal dash.
  JSON Schema `u` и HTML `v` сохраняют прежнее допустимое множество. Generated
  schema/validator обновлены штатно; browser `checkValidity` отклоняет пробелы
  и принимает исходные значения, unit сравнивает все три pattern в обоих режимах.
- #1292: ModalDialog назначает начальный focus через общий одноразовый callback
  с проверкой текущего focus. Два native autofocus в Projects/Integrations
  заменены маркером; Tab/Escape/return path сохранены. WebKit probe зафиксировал
  поздний переход owner → name без пользовательского действия до исправления.
- #1296: сравнение геометрии принимает только конечные DOMRect и погрешность
  менее 0.005px; больший сдвиг, отсутствие элемента, NaN/Infinity остаются FAIL.
- #1293: WebKit protocol может не отдавать File body в `request.postData`.
  Это помечается как NOT RUN конкретного readback; method/header и UI import
  проверяются. Отдельный loopback HTTP fixture получает реальные байты `File`
  от каждого движка, проверяет размер/digest и ровно один POST без credentials.
  Он не является проверкой настоящего Kodex artifact API или vendor upload.

Изменение runtime принадлежит только PWA `control-center`; canonical schema
потребляется сгенерированным PWA validator. Go/CP/RPC/DB/runner не меняются,
их общий rollout для этого PR не требуется. Формат schema, permissions и
допустимые значения прежние.

## История попыток

Приватные артефакты root: каталог делегирования `1274`; их имена и hashes
закреплены в PR. Ошибки и прерванные попытки сохраняются:

| Попытка              | Фактический результат                                                                                                                                                  |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| browser1             | Chromium 79 PASS/9 FAIL; Firefox 12 FAIL/2 timeout/2 interrupted/72 NOT RUN; WebKit 88 NOT RUN. Остановлена после общего preview Host blocker, exit 130                |
| browser2             | Chromium 90 PASS/3 FAIL; Firefox 82 PASS/11 FAIL; WebKit 19 PASS/2 interrupted/72 NOT RUN. Остановлена перед повторением известных platform/fixture failures, exit 130 |
| browser3, без Home   | Chromium 86/86 PASS; Firefox 86/86 PASS; WebKit 74 PASS/12 FAIL. Завершена, exit 1; второй capture терял instance shim mediaDevices                                    |
| webkit-voice4        | 9 PASS/12 FAIL. Промежуточная замена источника потока не устранила потерю shim; это не подтверждение проблемы AudioContext в приложении                                |
| browser5, Home/voice | Chromium 29/29 PASS; Firefox 21 PASS/7 FAIL/1 timeout; WebKit 22 PASS/7 FAIL, exit 1. Все 20 voice UI сценариев каждого движка прошли, Home выявил #1290/#1292         |
| focus7               | Диагностический WebKit Home: поздний native autofocus подтверждён событиями; далее FAIL #1293 до ответа upload fixture                                                 |

Дополнительные попытки:

- browser8: Chromium 9/9 PASS; Firefox 2 PASS/6 FAIL/1 timeout; WebKit 2 PASS/7 FAIL.
  Все три loopback POST byte/digest proofs прошли; выявлены оставшиеся
  browser diagnostics, вложенный microphone grant и субпиксельное сравнение.
- aborts10: 1 PASS/3 FAIL, сняты точные NS_BINDING_ABORTED/Load request cancelled.
- final-browser14: прерванный диагностический пакет, а не финальное evidence:
  Chromium 92 PASS/5 FAIL; Firefox 48 PASS/2 FAIL/3 interrupted/44 NOT RUN;
  WebKit 97 NOT RUN. Во время работы началась правка наблюдателя, поэтому partial
  результаты не объявляются проверкой итогового immutable head.
- observer15: 5 PASS/1 FAIL; binding AbortSignal мог приходить после network event.
- observer16: 6/6 PASS: Home 900 и File wire/AbortSignal proof в каждом движке.
- immutable abb83debf / browser18: независимые 267/267 PASS; Home 19 PASS/5 FAIL.
  Пять FAIL — неподтверждённая отмена PUT draft при явном reload страницы
  (Chromium 2900/900/768/390, Firefox 390). Все пользовательские assertions
  сохранения и readback прошли, итоговый diagnostics assert — FAIL.
- observer19: 6 PASS/3 FAIL; loopback File и abort body после headers прошли
  во всех движках. observer20: один Chromium Home900 FAIL с уточнённым PUT.
- observer21: явный reload учитывается как отмена уже существующего поколения,
  PUT FAIL исчез; Chromium Home900 остаётся FAIL из-за GET `/api/v1/bootstrap`,
  `net::ERR_ABORTED`, без подтверждённого поколения. Ошибка записана до финального
  assertion и teardown; момент относительно navigation не доказан.
  Пользовательские assertions прошли, но отсутствие пользовательского влияния
  этого запроса не доказано. Остаток сохраняется в #1285, полный Home не PASS.

Отдельная короткая `modal-pattern.synthetic.spec.ts` импортирует настоящий
ModalDialog и generated canonical schema. Она проверяет начальный и выбранный
focus, поздний callback, Tab/ShiftTab/Escape/возврат и native checkValidity
во всех трёх движках, независимо от общего Home observer.
Observer связывает START/ABORT с уникальным document/fetch ID; поздний abort
не присваивается следующему запросу того же URL. Неоднозначные поколения
не принимаются. Listener сохраняется после headers, поскольку body ещё может
быть отменён. Fetch input/init, Promise/Response и wire bytes не изменяются.

Первоначальный Linux capability readback: secure context и mediaDevices есть
во всех трёх движках; MediaRecorder есть в Chromium/Firefox. Chromium
принимает WebM/Opus и MP4, Firefox WebM/Opus и Ogg/Opus. Это browser
`isTypeSupported` boolean, не доказательство серверного decoder/provider.

## Ручная приёмка и риск

Оператор выбирает engine через `--project`, сохраняет JSON reporter отдельно
для новой попытки и сверяет annotations. После разрешённой выкладки владелец
проверяет настоящий microphone permission, capture/stop/cancel и вставку в
поддерживаемом браузере на `control.kodex.works`; данная задача новых платных
запросов не разрешает. Изменяются оснастка, HTML-compatible schema и управление начальным focus PWA.
Миграций, application authorization или production fallback нет. Rollback выполняется
обычным revert PR без изменения данных.

Context7: MDN HTML pattern Unicode Sets (`v`) и literal dash; Playwright 1.61.0 projects/browserName/launchOptions и различия
BrowserContext.grantPermissions; MDN AudioContext.createMediaStreamDestination
и MediaRecorder с generated stream. Дополнительные скрытые browser flags не
используются. Credentials, настоящее аудио/тексты и персональные данные не
использовались и не раскрывались.
