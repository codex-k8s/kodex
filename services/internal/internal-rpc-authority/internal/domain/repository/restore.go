package repository

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// RestoreCoordinationStore owns durable restore preparation, issuance, delivery, directives,
// acknowledgements and completion; transport messages do not independently establish progress.
// RestoreCoordinationStore владеет устойчивой подготовкой восстановления, выпуском, доставкой,
// директивами, подтверждениями и завершением; сообщения транспорта сами по себе не доказывают
// продвижение.
type RestoreCoordinationStore interface {
	// Prepare starts or resolves durable restore preparation from the validated command and returns the
	// authoritative coordination state.
	// Prepare начинает либо разрешает устойчивую подготовку восстановления по проверенной команде и
	// возвращает авторитетное состояние координации.
	Prepare(context.Context, model.PrepareRestoreCommand) (model.RestoreState, error)
	// Load reads the authoritative persisted restore state used to resume coordination instead of inferring
	// it from a previous transport response.
	// Load читает авторитетное устойчивое состояние восстановления для продолжения координации вместо вывода
	// по прежнему ответу транспорта.
	Load(context.Context) (model.RestoreState, error)
	// EnsureIssuance records or resolves the issuance entry for the identified restore operation, retaining
	// the existing result on an admissible repeat.
	// EnsureIssuance фиксирует либо разрешает запись выпуска указанной операции восстановления, сохраняя
	// существующий результат при допустимом повторе.
	EnsureIssuance(
		context.Context,
		string,
		model.RestoreIssuanceRecord,
	) (model.RestoreIssuanceRecord, error)
	// RecordDelivery persists delivery evidence for the identified restore operation and returns its updated
	// coordination state; delivery is distinct from role acknowledgement.
	// RecordDelivery сохраняет свидетельство доставки указанной операции восстановления и возвращает её
	// состояние; доставка отличается от подтверждения роли.
	RecordDelivery(
		context.Context,
		string,
		model.RestoreDeliveryRecord,
	) (model.RestoreState, error)
	// SaveDirective stores the directive associated with the identified restore operation and returns the
	// resulting state for subsequent delivery and acknowledgement checks.
	// SaveDirective сохраняет директиву указанной операции восстановления и возвращает состояние для
	// последующей доставки и проверки подтверждений.
	SaveDirective(
		context.Context,
		string,
		model.RestoreDirectiveRecord,
	) (model.RestoreDirectiveRecord, error)
	// RecordACK records a role acknowledgement against the identified restore operation and returns both
	// authoritative coordination state and the resolved acknowledgement record.
	// RecordACK фиксирует подтверждение роли для указанной операции восстановления и возвращает авторитетное
	// состояние координации и разрешённую запись подтверждения.
	RecordACK(
		context.Context,
		string,
		model.RestoreACKRecord,
	) (model.RestoreState, model.RestoreACKRecord, error)
	// AuthorizeOperator records consumption of the verified operator authorization binding; possessing a
	// request payload alone does not authorize a restore command.
	// AuthorizeOperator фиксирует потребление проверенной привязки полномочий оператора; одних полей запроса
	// недостаточно для команды восстановления.
	AuthorizeOperator(
		context.Context,
		model.RestoreOperatorAuthorizationRecord,
	) error
	// Complete applies the validated completion command to durable restore coordination; evidence and
	// readiness requirements remain part of the domain workflow.
	// Complete применяет проверенную команду завершения к устойчивой координации восстановления; требования
	// свидетельств и готовности остаются частью доменного процесса.
	Complete(context.Context, model.CompleteRestoreCommand) (model.RestoreState, error)
	// CoordinationReady checks the restore-coordination storage path before it is used; it is not a
	// substitute for completion evidence of a particular restore.
	// CoordinationReady проверяет путь хранилища координации перед использованием; это не заменяет
	// свидетельства завершения конкретного восстановления.
	CoordinationReady(context.Context) error
}

// RestoreOperatorCredentialVerifier authenticates a projected ServiceAccount credential through
// Kubernetes TokenReview with the required audience and exact operator identity.
// RestoreOperatorCredentialVerifier проверяет проецируемые учётные данные ServiceAccount через
// Kubernetes TokenReview с требуемой аудиторией и точной личностью оператора.
type RestoreOperatorCredentialVerifier interface {
	// VerifyOperatorCredential validates the supplied token for the supplied audience and returns
	// authenticated operator claims, not an identity copied from the command body.
	// VerifyOperatorCredential проверяет переданный токен для указанной аудитории и возвращает
	// подтверждённые данные оператора, а не личность из тела команды.
	VerifyOperatorCredential(
		context.Context,
		string,
		string,
	) (model.RestoreOperatorCredential, error)
}

// RestoreEvidenceVerifier validates independently signed completion evidence against the selected
// restore state; unverified report contents cannot lift the fence.
// RestoreEvidenceVerifier проверяет независимо подписанное свидетельство завершения относительно
// выбранного состояния восстановления; непроверенный отчёт не снимает блокировку.
type RestoreEvidenceVerifier interface {
	// VerifyCompletedEvidence reads and verifies completion evidence for the exact restore state and returns
	// the verified result or an error without authorizing completion by itself.
	// VerifyCompletedEvidence читает и проверяет свидетельство завершения точного состояния восстановления и
	// возвращает проверенный результат либо ошибку, само по себе не разрешая завершение.
	VerifyCompletedEvidence(
		context.Context,
		model.RestoreState,
	) (model.RestoreCompletionEvidence, error)
}

// RestoreFenceStore applies and checks the runtime fence for the exact restore state so control-plane
// coordination also has an enforceable serving boundary.
// RestoreFenceStore применяет и проверяет рабочую блокировку точного состояния восстановления, связывая
// координацию с исполняемой границей обслуживания.
type RestoreFenceStore interface {
	// ApplyRestoreFence installs the fence represented by the supplied restore state; successful return does
	// not replace the separate readiness check.
	// ApplyRestoreFence устанавливает блокировку указанного состояния восстановления; успешный возврат не
	// заменяет отдельную проверку готовности.
	ApplyRestoreFence(context.Context, model.RestoreState) error
	// RestoreFenceReady verifies that the serving fence matches the supplied restore state instead of
	// trusting only the preceding write response.
	// RestoreFenceReady проверяет соответствие рабочей блокировки указанному состоянию восстановления вместо
	// доверия только ответу предыдущей записи.
	RestoreFenceReady(context.Context, model.RestoreState) error
}

// RestoreCredentialPublisher delivers restore credentials through the managed role-bound publication
// path rather than exposing credential material to the coordination caller.
// RestoreCredentialPublisher доставляет учётные данные восстановления через управляемую публикацию с
// привязкой к роли, не выдавая материал вызывающей стороне координации.
type RestoreCredentialPublisher interface {
	// PublishRoleCredential publishes the referenced role credential using the supplied idempotency binding
	// and returns delivery evidence for coordination, not the secret value.
	// PublishRoleCredential публикует указанное учётное полномочие роли с заданной привязкой идемпотентности
	// и возвращает свидетельство доставки для координации, а не секрет.
	PublishRoleCredential(
		context.Context,
		string,
		string,
	) (model.RestoreDeliveryRecord, error)
	// PublisherReady checks the restore credential publisher's operational path before delivery is
	// attempted; it does not prove any particular credential was delivered.
	// PublisherReady проверяет рабочий путь публикации учётных данных восстановления до доставки; это не
	// доказывает доставку конкретных данных.
	PublisherReady(context.Context) error
}
