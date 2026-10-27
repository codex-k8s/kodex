package platform

import _ "embed"

var (
	// queryInteractionListSources Embeds at most 100 active matching-definition Mattermost source
	// connections with enabled inbound/gate grants, active projects and credential-revision metadata.
	// Package capability validation remains in the caller; returned references contain no credential
	// plaintext.
	// queryInteractionListSources Встраивает выборку не более 100 активных соединений Mattermost с
	// совпавшими версиями пакета, включёнными входящими разрешениями или решениями шлюзов, активными
	// проектами и метаданными ревизии credential. Возможности пакета проверяет вызывающий; значений
	// учётных данных выборка не содержит.
	//go:embed sql/interaction_list_sources.sql
	queryInteractionListSources string
	// queryInteractionClaimDeliveries Expires stale claims to UNKNOWN_OUTCOME, then locks due/failed
	// eligible deliveries and current connection/grant rows with SKIP LOCKED, validates required
	// approvals/identity pins and creates 45-second leases with incremented attempt/generation and
	// hashed fences. Unknown outcomes are not selected for resend; callers validate package
	// capabilities before commit.
	// queryInteractionClaimDeliveries Переводит истёкшие захваты в UNKNOWN_OUTCOME, блокирует
	// пригодные DUE/FAILED доставки и текущие соединения с разрешениями через SKIP LOCKED, проверяет
	// необходимые одобрения и привязки личности. Создаёт 45-секундные аренды с ростом
	// attempt/generation и хешем fence. Неопределённый исход не выбирается для повтора; пакет
	// проверяется до commit.
	//go:embed sql/interaction_claim_deliveries.sql
	queryInteractionClaimDeliveries string
	// queryInteractionCompleteDeliveryResolve Locks a claimed delivery only under exact
	// organization/ref/lease/fence digest/generation and an unexpired database-clock deadline,
	// returning owner/run/gate/target metadata. It does not independently match workload_instance;
	// that boundary remains outside this query.
	// queryInteractionCompleteDeliveryResolve Блокирует CLAIMED доставку при точном совпадении
	// организации, ссылки, аренды, хеша fence, поколения и неистёкшего срока по часам БД. Возвращает
	// сведения владельца, запуска, шлюза и назначения. Собственного условия workload_instance нет; эта
	// граница проверяется снаружи.
	//go:embed sql/interaction_complete_delivery_resolve.sql
	queryInteractionCompleteDeliveryResolve string
	// queryInteractionCompleteDeliveryUpdate Updates a resolved locked delivery to SUCCEEDED, FAILED
	// only for confirmed-no-effect, or UNKNOWN_OUTCOME otherwise, clearing the lease and increasing
	// version. Success records external targets; only confirmed-no-effect schedules bounded backoff.
	// The caller owns the preceding fence check.
	// queryInteractionCompleteDeliveryUpdate Переводит разрешённую заблокированную доставку в
	// SUCCEEDED, в FAILED только при подтверждённом отсутствии эффекта, иначе в UNKNOWN_OUTCOME.
	// Сбрасывает аренду и увеличивает версию; успех сохраняет внешнее назначение, подтверждённое
	// отсутствие эффекта задаёт ограниченную задержку повтора. Fence проверяется вызывающим заранее.
	//go:embed sql/interaction_complete_delivery_update.sql
	queryInteractionCompleteDeliveryUpdate string
	// queryInteractionFindGateDelivery Finds a succeeded gate-decision delivery at exact
	// organization/connection/team/channel/root-post coordinates with current enabled package/grant
	// pins and locks its owner gate. It does not require OPEN state so callers can persist stale
	// receipts.
	// queryInteractionFindGateDelivery Находит успешную доставку решения шлюза по точным организации,
	// соединению, команде, каналу и корневому сообщению с текущими включёнными привязками пакета и
	// разрешения, блокирует owner gate. Не требует OPEN, позволяя вызывающему записать устаревший
	// ответ.
	//go:embed sql/interaction_find_gate_delivery.sql
	queryInteractionFindGateDelivery string
	// queryInteractionListInboundGrants Locks at most two current inbound grants for the exact active
	// Mattermost connection and active project, requiring a ready enabled agent with published
	// instructions or a published workflow. Two rows expose ambiguity; command launch authority is
	// checked separately.
	// queryInteractionListInboundGrants Блокирует до двух текущих входящих разрешений точного
	// активного соединения Mattermost и активного проекта. Требует готового включённого агента с
	// опубликованными инструкциями либо опубликованный workflow. Две строки выявляют неоднозначный
	// маршрут; право запуска проверяется отдельно.
	//go:embed sql/interaction_list_inbound_grants.sql
	queryInteractionListInboundGrants string
	// queryInteractionFindMessageReceipt Looks up prior processing only under exact organization,
	// connection, external-event digest, resolved subject and interaction identity, returning
	// outcome/run/gate refs. Deduplication does not authorize a new effect or bypass current identity
	// resolution.
	// queryInteractionFindMessageReceipt Ищет прежний результат только по точным организации,
	// соединению, хешу внешнего события, разрешённому субъекту и личности взаимодействия. Возвращает
	// исход и ссылки запуска или шлюза; дедупликация не разрешает новый эффект и не обходит свежую
	// проверку личности.
	//go:embed sql/interaction_find_message_receipt.sql
	queryInteractionFindMessageReceipt string
	// queryInteractionInsertMessageReceipt Atomically inserts the resolved subject/identity receipt
	// with external coordinates and digests, then enqueues an acknowledgement only when
	// project/grant/root-run links exist. The caller transaction owns authorization and commit; SQL
	// does not send an external message.
	// queryInteractionInsertMessageReceipt Атомарно вставляет квитанцию разрешённых субъекта и
	// личности с внешними координатами и хешами, затем ставит подтверждение в очередь только при
	// наличии проекта, разрешения и корневого запуска. Авторизация и commit принадлежат транзакции
	// вызывающего; внешнее сообщение SQL не отправляет.
	//go:embed sql/interaction_insert_message_receipt.sql
	queryInteractionInsertMessageReceipt string
	// queryInteractionResolveConnection Resolves an enabled connected/degraded Mattermost connection
	// by exact organization/ref. This narrow lookup has no lifecycle/package/grant predicate; the
	// following package and identity checks remain required.
	// queryInteractionResolveConnection Разрешает включённое соединение Mattermost в
	// CONNECTED/DEGRADED по точным организации и ссылке. Узкий поиск не проверяет жизненный цикл,
	// пакет или разрешение; последующие проверки пакета и личности обязательны.
	//go:embed sql/interaction_resolve_connection.sql
	queryInteractionResolveConnection string
	// queryInteractionEnqueueGateDeliveries Inserts DUE gate-decision deliveries for current matching
	// enabled grants/connections with bounded gate context and conflict suppression. Resolved
	// gate/root/project IDs are caller-owned inputs; enqueue performs no external send.
	// queryInteractionEnqueueGateDeliveries Вставляет DUE доставки решений шлюза для текущих совпавших
	// включённых разрешений и соединений, ограничивает контекст и подавляет конфликт вставки.
	// Разрешённые ID шлюза, корневого запуска и проекта задаёт вызывающий; внешней отправки нет.
	//go:embed sql/interaction_enqueue_gate_deliveries.sql
	queryInteractionEnqueueGateDeliveries string
	// queryInteractionCancelPendingGateDeliveries Closes due/failed gate deliveries as CANCELLED and
	// claimed ones as UNKNOWN_OUTCOME, revoking lease/fence/workload metadata and incrementing
	// version. The resolved gate ID is the entire SQL scope; tenant/authority resolution is owned by
	// callers.
	// queryInteractionCancelPendingGateDeliveries Закрывает DUE/FAILED доставки шлюза как CANCELLED,
	// захваченные — как UNKNOWN_OUTCOME, отзывает аренду, fence и workload, увеличивает версию. SQL
	// ограничен разрешённым ID шлюза; организацию и полномочия устанавливает вызывающий.
	//go:embed sql/interaction_cancel_pending_gate_deliveries.sql
	queryInteractionCancelPendingGateDeliveries string
	// queryInteractionListFailedIncidents Reads at most 100 recent organization delivery
	// failures/unknown outcomes or successes after retry, including attempt limits for incident
	// projection. This internal organization-scoped query adds no actor-level run eligibility.
	// queryInteractionListFailedIncidents Читает до 100 недавних неуспешных или неопределённых
	// доставок организации и успехов после повтора с пределами попыток для проекции инцидента.
	// Внутренняя выборка организации не добавляет проверку доступа субъекта к запуску.
	//go:embed sql/interaction_list_failed_incidents.sql
	queryInteractionListFailedIncidents string
	// queryInteractionListRunIncidents Projects recent delivery incidents of the requested run root
	// under exact organization/run and owner/admin or active project VIEW membership. The SQL uses
	// this explicit legacy eligibility rule rather than the general access-check function.
	// queryInteractionListRunIncidents Проецирует недавние инциденты доставки корня указанного запуска
	// по точным организации и ссылке при OWNER/ADMINISTRATOR либо активном членстве проекта с VIEW.
	// SQL использует это явное прежнее правило, а не общую функцию проверки доступа.
	//go:embed sql/interaction_list_run_incidents.sql
	queryInteractionListRunIncidents string
	// queryInteractionCountActiveAdapters Counts distinct enabled connected/degraded Mattermost
	// connections with one enabled supported grant in the organization. It is a coarse adapter count,
	// without current definition-pin/lifecycle or external-health validation.
	// queryInteractionCountActiveAdapters Считает различные включённые соединения Mattermost в
	// CONNECTED/DEGRADED с одним включённым поддерживаемым разрешением организации. Это
	// приблизительный счётчик адаптеров без проверки текущих версий пакета, жизненного цикла и
	// внешнего здоровья.
	//go:embed sql/interaction_count_active_adapters.sql
	queryInteractionCountActiveAdapters string
	// queryInteractionTerminalCandidates Locks current connection/grant candidates for
	// notifications/result mirroring of an exact terminal root run/project, excluding existing
	// same-capability deliveries. Candidate selection requires later human-per-effect approval and is
	// not permission to send.
	// queryInteractionTerminalCandidates Блокирует текущие соединения и разрешения для уведомлений или
	// зеркала результата точного завершённого корневого запуска проекта, исключая существующую
	// доставку той же возможности. Кандидату ещё требуется одобрение каждого эффекта человеком;
	// отправка не разрешается.
	//go:embed sql/interaction_terminal_candidates.sql
	queryInteractionTerminalCandidates string
	// queryInteractionTerminalCreateApproval Atomically creates a HUMAN_GATE node, owner approval gate
	// and WAITING_APPROVAL delivery pinned to connection/definition versions with bounded
	// context/result text and attempt policy. No external effect is performed and the caller supplies
	// already resolved ownership IDs.
	// queryInteractionTerminalCreateApproval Атомарно создаёт узел HUMAN_GATE, owner gate одобрения и
	// WAITING_APPROVAL доставку с привязками версий соединения и пакета, ограниченным контекстом и
	// результатом, правилом попыток. Внешнего эффекта нет; разрешённые ID владения задаёт вызывающий.
	//go:embed sql/interaction_terminal_create_approval.sql
	queryInteractionTerminalCreateApproval string
	// queryInteractionApprovalResolve Locks exact organization/gate delivery, connection and grant,
	// returning gate/version/state plus stored definition pins and current grant validity. The caller
	// decides legal transitions and authority; the boolean is not an independent approval.
	// queryInteractionApprovalResolve Блокирует доставку, соединение и разрешение точного шлюза
	// организации, возвращает версию и состояние шлюза, сохранённые привязки пакета и пригодность
	// текущего разрешения. Допустимость перехода и полномочия проверяет вызывающий; boolean сам не
	// является одобрением.
	//go:embed sql/interaction_approval_resolve.sql
	queryInteractionApprovalResolve string
	// queryInteractionApprovalUpdate Updates only a resolved WAITING_APPROVAL delivery to the
	// caller-selected state/error, setting completion time only for CANCELLED and incrementing
	// version. It has no independent actor or expected-version predicate.
	// queryInteractionApprovalUpdate Меняет только разрешённую WAITING_APPROVAL доставку на заданные
	// вызывающим состояние и код, ставит время завершения лишь для CANCELLED и увеличивает версию.
	// Собственного условия по субъекту или ожидаемой версии нет.
	//go:embed sql/interaction_approval_update.sql
	queryInteractionApprovalUpdate string
	// queryInteractionApprovalInvalidate Revokes open approval-backed deliveries for an exact
	// organization/connection: claimed effects become UNKNOWN_OUTCOME and other pending states
	// CANCELLED, with lease cleanup. Returns and locks still-open linked gates so the caller can close
	// the graph atomically; it never resends.
	// queryInteractionApprovalInvalidate Отзывает незавершённые доставки с одобрением точного
	// соединения организации: захваченные становятся UNKNOWN_OUTCOME, прочие ожидающие — CANCELLED,
	// аренды очищаются. Возвращает и блокирует связанные открытые шлюзы для атомарного закрытия графа
	// вызывающим; повторной отправки нет.
	//go:embed sql/interaction_approval_invalidate.sql
	queryInteractionApprovalInvalidate string
)
