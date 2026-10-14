---
id: OPS-DOC-RECOVERY-001
title: Контрольные точки и продолжение разработки после таймаута
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-14
---

# Причина и решение

В локальном dogfooding Issue1796 попытка10 завершилась по immutable deadline
этапа IMPLEMENTATION, спустя7200s после начала11:46:35 UTC. OOM/restart не
наблюдались. Developer самостоятельно создал Draft1809 и отправил checkpoint
a00706022058573157fe50bf431aa67485507b03; это не final acceptance.

Исторический run и его часы не изменяются. Для будущего запуска публикуется
новая версия процесса с бюджетом IMPLEMENTATION14400s. Editable инструкции
Developer и Manager дополняются общим правилом сохранения промежуточных
результатов и восстановления существующей задачи, без конкретного business
кода, выдачи новых полномочий или корректирующего USER сообщения.

| Переход | Полномочия и проверка | Результат |
| --- | --- | --- |
| Owner → instruction draft | Проверенная SSO, eligible Agent, If-Match и idempotency | Новый draft; прежняя published версия неизменна |
| Validate → impact → publish | Exact draft/source версии, явный выбор только binding этого Agent | Новая опубликованная инструкция для будущих RuntimeRevision |
| Owner → Workflow draft → validate → publish | Eligible Workflow, If-Match, неизменные роли/grants/один Reviewer/owner gate | Новый timeout только будущего этапа; старые run clocks неизменны |
| Retry | Штатная команда exact FAILED run, fresh version и root actor | Новая attempt/session/RuntimeRevision; прежние terminal leases закрыты |
| Native recovery | Разрешённые READ исходного задания/предыдущих результатов/собственного PR и Git readback | Собственный проверенный checkpoint, либо явный UNKNOWN/BLOCKED |

Не читать значения credential и не переносить их в evidence. Не продолжать
отклонённый WRITE; UNKNOWN_OUTCOME разрешается только точным readback.
Нельзя записывать бизнес-код на host, переносить незакоммиченные изменения
чужого execution, менять clock вручную или объявлять checkpoint готовностью.
Смена бюджета не разрешает второй comprehensive Review или business merge.

# Checklist

- [x] Причина установлена: RUNTIME_TIMEOUT, root/child terminal и сохранённый remote checkpoint.
- [x] Editable инструкции сохранены, validated, опубликованы и exact binding readback подтверждён.
- [x] Future Workflow publication и сохранение прочих security/lifecycle полей подтверждены.
- [x] Штатное продолжение исходной задачи, не новый business prompt.
- [ ] Реальный final Developer → один Reviewer → Manager/owner.

Readback13:55–13:59 UTC: Developer revision4/agent44/binding4,
Manager revision5/agent45/binding5, Reviewer revision4/agent42/binding4;
все три VALID→impact(total1)→PUBLISHED/APPLIED, exact target SHA MATCH.
Manager scoped пять циклов заменены единственным Review; Reviewer получил
явное scoped исключение перед общими правилами других задач.
Workflow49/revision16/wfv_Bctunk4SNG_NahT4TNKlkIcb PUBLISHED.
Все semantic fields, роли, capability lists, порядок и owner gate MATCH;
изменён только IMPLEMENTATION7200→14400s. Публичный editor назначил новые
ключи Reviewer step-004 и final Manager step-005 вместо прежних step-040/
step-004: это новые server-owned keys будущей revision, не изменение
исторического графа. Fresh Retry11/run_4E4x4FCjIg44_NnXl35XGlil RUNNING,
retryOf original10, прежняя root session сохранена. Новый бизнес-текст и
host business implementation отсутствуют; final/Review пока NOT RUN.

# Дополнение editable инструкций

При повторе того же задания сначала разрешёнными READ найди прежние попытки,
результаты и рабочий PR. Проверь repository, Issue, автора, base/head SHA,
branch, актуальные grants и отсутствие другого активного писателя. При
доказанном происхождении продолжай прежнюю собственную рабочую ветку в
отдельном проверенном worktree; не создавай дублирующий PR только потому, что
сменились attempt или session. Не угадывай прежний SHA и не заимствуй чужой
workspace. Если происхождение или результат эффекта неизвестны, сначала
точный readback; при недоступности зафиксируй UNKNOWN/BLOCKED.

Сохраняй маленький checkpoint прогресса не реже15 минут, включая диапазоны
собственного чтения и оставшиеся проверки. Git commit/push только при
действующих правах и безопасном содержимом. После небольшого законченного
изменения не откладывай быструю проверку и Git checkpoint до полного codegen
или всего baseline. Перед длительной проверкой зафиксируй уже сделанное.
Восстановленный checkpoint не означает PASS нового SHA. Не перечитывай
собственные доказанно полные unchanged входы; требуемые независимые чтения
Reviewer и fresh authority/version проверки сохраняются.

Для dogfooding1796/1797 ровно один comprehensive Review; без автоматических
fix/re-review циклов. Manager передаёт findings, FAIL/NOT RUN и непроверенный
SHA владельцу, не объявляет их устранёнными или READY. Это scoped исключение
не отменяет общие правила для других задач и не разрешает business merge.
