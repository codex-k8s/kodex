package platform

import _ "embed"

var (
	// queryAttachmentSetsSelectArtifacts reads exact ACTIVE/CLEAN tenant/project artifacts using
	// request ordinality, preserving requested order and artifact version/content pins.
	// queryAttachmentSetsSelectArtifacts читает точные ACTIVE/CLEAN tenant/project artifacts по
	// request ordinality, сохраняя порядок запроса и artifact version/content pins.
	//go:embed sql/attachmentsets_select_artifacts.sql
	queryAttachmentSetsSelectArtifacts string
	// queryAttachmentSetsInsertSet creates a revision with version equal to revision number and
	// optional manifest, assigning finalized_at only for FINALIZED state.
	// queryAttachmentSetsInsertSet создаёт ревизию с version, равной её номеру, и необязательным
	// manifest, задавая finalized_at только для FINALIZED state.
	//go:embed sql/attachmentsets_insert_set.sql
	queryAttachmentSetsInsertSet string
	// queryAttachmentSetsInsertItem stores one ordered immutable artifact revision descriptor and its
	// internal ID under the exact attachment set.
	// queryAttachmentSetsInsertItem сохраняет один упорядоченный неизменяемый artifact revision
	// descriptor и его внутренний ID в точном attachment set.
	//go:embed sql/attachmentsets_insert_item.sql
	queryAttachmentSetsInsertItem string
	// queryAttachmentSetsInsertBinding stores tenant/project/creator and the purpose-specific
	// assistant/session/run/gate target columns supplied by the owner.
	// queryAttachmentSetsInsertBinding сохраняет tenant/project/creator и переданные владельцем
	// purpose-specific target columns для assistant/session/run/gate.
	//go:embed sql/attachmentsets_insert_binding.sql
	queryAttachmentSetsInsertBinding string
	// queryAttachmentSetsBindRun sets a run input-attachment pointer only while no set is bound,
	// leaving affected-row confirmation to the caller.
	// queryAttachmentSetsBindRun задаёт input-attachment pointer run только при отсутствии
	// привязанного набора, оставляя проверку affected rows caller.
	//go:embed sql/attachmentsets_bind_run.sql
	queryAttachmentSetsBindRun string
	// queryAttachmentSetsBindTurn sets the session-turn attachment pointer only when null, preserving
	// a previously bound set.
	// queryAttachmentSetsBindTurn задаёт attachment pointer session-turn только при null, сохраняя
	// ранее привязанный набор.
	//go:embed sql/attachmentsets_bind_turn.sql
	queryAttachmentSetsBindTurn string
	// queryAttachmentSetsBindGateResolution sets an owner-gate resolution attachment pointer only when
	// null; this SQL alone does not approve or resolve the gate.
	// queryAttachmentSetsBindGateResolution задаёт attachment pointer owner-gate resolution только при
	// null; сам SQL не одобряет и не разрешает gate.
	//go:embed sql/attachmentsets_bind_gate_resolution.sql
	queryAttachmentSetsBindGateResolution string
	// queryAttachmentSetsSelectTimestamps reads database-assigned creation and optional finalization
	// times for the newly inserted set ID.
	// queryAttachmentSetsSelectTimestamps читает назначенные БД creation и необязательное finalization
	// time для ID нового набора.
	//go:embed sql/attachmentsets_select_timestamps.sql
	queryAttachmentSetsSelectTimestamps string
	// queryAttachmentSetsSelectFamily resolves a family only for an exact tenant/set reference created
	// by the current actor before family mutation locking.
	// queryAttachmentSetsSelectFamily разрешает семейство только для точной tenant/set reference,
	// созданной текущим actor, до блокировки изменения семейства.
	//go:embed sql/attachmentsets_select_family.sql
	queryAttachmentSetsSelectFamily string
	// queryAttachmentSetsLockFamily takes a transaction-scoped advisory lock on the family hash so
	// revision mutation serializes under the owner transaction.
	// queryAttachmentSetsLockFamily берёт транзакционную advisory lock по хешу семейства для
	// последовательного изменения ревизий в транзакции владельца.
	//go:embed sql/attachmentsets_lock_family.sql
	queryAttachmentSetsLockFamily string
	// queryAttachmentSetsSelectLatest returns the family maximum revision only when the requested
	// reference itself is that current revision, with exact project and lifecycle fields.
	// queryAttachmentSetsSelectLatest возвращает максимальную ревизию семейства только если
	// запрошенная reference сама является текущей ревизией, с точными project и lifecycle fields.
	//go:embed sql/attachmentsets_select_latest.sql
	queryAttachmentSetsSelectLatest string
	// queryAttachmentSetsResolveFinalized resolves a FINALIZED exact tenant/project/purpose set only
	// when no newer family revision exists.
	// queryAttachmentSetsResolveFinalized разрешает FINALIZED set точных tenant/project/purpose только
	// при отсутствии более новой ревизии семейства.
	//go:embed sql/attachmentsets_resolve_finalized.sql
	queryAttachmentSetsResolveFinalized string
	// queryAttachmentSetsLockMaterializableItems joins immutable artifact/content pins, requires CLEAN
	// and ACTIVE or explicitly allowed DELETED state, and locks heads in stable artifact order.
	// queryAttachmentSetsLockMaterializableItems связывает неизменяемые artifact/content pins, требует
	// CLEAN и ACTIVE либо явно допустимый DELETED state и блокирует heads в устойчивом порядке
	// artifact.
	//go:embed sql/attachmentsets_lock_materializable_items.sql
	queryAttachmentSetsLockMaterializableItems string
	// queryAttachmentSetsListItems reads all saved item snapshots in position order without
	// independently assigning artifact access or lifecycle eligibility.
	// queryAttachmentSetsListItems читает все сохранённые snapshots элементов по position без
	// самостоятельного назначения artifact access либо lifecycle eligibility.
	//go:embed sql/attachmentsets_list_items.sql
	queryAttachmentSetsListItems string
	// queryAttachmentSetsGet reads exact tenant/set metadata and authoritative superseded status;
	// organization/project and item access are checked by the caller.
	// queryAttachmentSetsGet читает точные tenant/set metadata и авторитетный superseded status;
	// organization/project и item access проверяет caller.
	//go:embed sql/attachmentsets_get.sql
	queryAttachmentSetsGet string
	// queryAttachmentSetsGetItems reads a bounded ordered page after an exact item position; cursor
	// parsing and access checks remain outside the SQL.
	// queryAttachmentSetsGetItems читает ограниченную упорядоченную страницу после точной position;
	// разбор cursor и проверки доступа остаются вне SQL.
	//go:embed sql/attachmentsets_get_items.sql
	queryAttachmentSetsGetItems string
	// queryAttachmentSetsProjectByRef resolves the public project locator of an exact tenant/set
	// reference, preserving empty organization-level project scope.
	// queryAttachmentSetsProjectByRef разрешает публичный project locator точной tenant/set reference,
	// сохраняя пустую project scope уровня организации.
	//go:embed sql/attachmentsets_project_by_ref.sql
	queryAttachmentSetsProjectByRef string
)
