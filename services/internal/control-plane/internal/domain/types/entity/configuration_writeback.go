package entity

import "time"

const (
	// WriteBackWaiting marks a Git write-back proposal waiting for an exact human decision. No external
	// effect is authorized merely by preparing the proposal.
	// WriteBackWaiting обозначает предложение обратной записи в Git, ожидающее точного решения человека.
	// Подготовка предложения сама по себе не разрешает внешний эффект.
	WriteBackWaiting = "WAITING_APPROVAL"
	// WriteBackQueued marks approved write-back work waiting for a claim. The owner must still validate
	// current source and connection pins before an effect starts.
	// WriteBackQueued обозначает подтверждённую обратную запись, ожидающую захвата. Перед началом эффекта
	// владелец проверяет текущие привязки источника и подключения.
	WriteBackQueued = "QUEUED"
	// WriteBackClaimed marks write-back work held by a fenced worker lease. Claiming work is distinct from
	// recording the start of an external effect.
	// WriteBackClaimed обозначает работу обратной записи, удерживаемую ограждённой арендой исполнителя.
	// Захват работы отличается от фиксации начала внешнего эффекта.
	WriteBackClaimed = "CLAIMED"
	// WriteBackStarted marks the owner-recorded start of an external write-back effect. A lost response
	// after this point cannot be classified as confirmed absence of effect without reconciliation.
	// WriteBackStarted обозначает зафиксированное владельцем начало внешнего эффекта обратной записи.
	// Потерю ответа после этого нельзя считать подтверждённым отсутствием эффекта без сверки.
	WriteBackStarted = "EFFECT_STARTED"
	// WriteBackSucceeded marks owner-confirmed successful completion of the write-back proposal. A worker
	// claim or candidate commit alone does not establish this state.
	// WriteBackSucceeded обозначает подтверждённое владельцем успешное завершение предложения обратной
	// записи. Сам захват работы или коммит-кандидат этого состояния не устанавливает.
	WriteBackSucceeded = "SUCCEEDED"
	// WriteBackRejected marks a rejected write-back proposal. It records the proposal decision rather than
	// a rejection of unrelated configuration content.
	// WriteBackRejected обозначает отклонённое предложение обратной записи. Это решение по предложению, а
	// не отклонение несвязанного содержимого конфигурации.
	WriteBackRejected = "REJECTED"
	// WriteBackCancelled marks cancellation recorded by the write-back owner. It is not evidence that a
	// previously started remote branch or pull-request effect has been undone.
	// WriteBackCancelled обозначает отмену, зафиксированную владельцем обратной записи. Это не
	// свидетельство отмены уже начатого удалённого эффекта ветки или pull request.
	WriteBackCancelled = "CANCELLED"
	// WriteBackExpired marks a write-back proposal whose allowed lifetime expired. The expired approval
	// cannot authorize a new external effect.
	// WriteBackExpired обозначает предложение обратной записи с истёкшим допустимым сроком. Истёкшее
	// подтверждение не разрешает новый внешний эффект.
	WriteBackExpired = "EXPIRED"
	// WriteBackFailed marks a write-back failure classified by the owner. It is distinct from an uncertain
	// external outcome and does not imply that arbitrary retries are safe.
	// WriteBackFailed обозначает ошибку обратной записи, классифицированную владельцем. Она отличается от
	// неопределённого внешнего исхода и не означает безопасность произвольных повторов.
	WriteBackFailed = "FAILED"
	// WriteBackUnknown preserves an uncertain outcome of a started external write-back effect. Recovery
	// must reconcile evidence rather than blindly repeating the effect.
	// WriteBackUnknown сохраняет неопределённый исход начатого внешнего эффекта обратной записи.
	// Восстановление сверяет свидетельства, а не повторяет эффект вслепую.
	WriteBackUnknown = "UNKNOWN_OUTCOME"
	// WriteBackBranch identifies the remote branch effect within a write-back proposal. The effect has its
	// own start and confirmation evidence, separate from creating a pull request.
	// WriteBackBranch определяет внешний эффект ветки в предложении обратной записи. У эффекта собственные
	// свидетельства начала и подтверждения, отдельные от создания pull request.
	WriteBackBranch = "BRANCH"
	// WriteBackPullRequest identifies the remote pull-request effect after branch confirmation. It is an
	// effect kind rather than an overall proposal lifecycle state.
	// WriteBackPullRequest определяет внешний эффект pull request после подтверждения ветки. Это вид
	// эффекта, а не общее состояние жизненного цикла предложения.
	WriteBackPullRequest = "PULL_REQUEST"
)

// ConfigurationWriteBackAction presents an available proposal action and a reason when it is disabled.
// Enabled describes the current projection and does not replace server-side checks when the action is
// requested.
// ConfigurationWriteBackAction представляет доступное действие предложения и причину его
// недоступности. Enabled описывает текущую проекцию и не заменяет серверные проверки при запросе
// действия.
type ConfigurationWriteBackAction struct {
	Action, Reason string
	Enabled        bool
}

// ConfigurationWriteBack is the public proposal projection with exact configuration, source,
// connection, content, approval, and external-effect pins and timestamps. It excludes private packages
// and credential locators; lifecycle changes still require owner validation.
// ConfigurationWriteBack является публичной проекцией предложения с точными привязками конфигурации,
// источника, подключения, содержимого, подтверждения, внешних эффектов и времени. Закрытые пакеты и
// адреса реквизитов исключены; изменения жизненного цикла проверяет владелец.
type ConfigurationWriteBack struct {
	Ref, ConfigurationRef, Kind, SourceRef, ConnectionRef                  string
	Version, ConfigurationVersion, SourceVersion, ConnectionVersion        int64
	RepositoryRef, SourceRefName, Path, BaseCommitSHA, BaseContentSHA256   string
	ProposedContentSHA256, ContentFormat, ProposalBranch, ApprovalDigest   string
	State, FailureCode, CandidateCommitSHA, PullRequestRef, PullRequestURL string
	CreatedAt, ExpiresAt                                                   time.Time
	ApprovedAt, CompletedAt, BranchConfirmedAt, PullRequestConfirmedAt     *time.Time
	NextActions                                                            []ConfigurationWriteBackAction
}

// ConfigurationWriteBackView pairs a public proposal with its base and proposed configuration text for
// inspection. Text may contain private business data; the view is not an approval or an automatically
// publishable log record.
// ConfigurationWriteBackView связывает публичное предложение с исходным и предлагаемым текстом
// конфигурации для просмотра. Текст может содержать закрытые рабочие данные; представление не является
// подтверждением или автоматически допустимой записью лога.
type ConfigurationWriteBackView struct {
	Proposal                     ConfigurationWriteBack
	BaseContent, ProposedContent string
}

// ConfigurationWriteBackLease identifies a proposal claim by claimant, fence, attempt, generation, and
// expiry. Every worker effect report must be checked against the owner's current lease rather than
// trusting these copied values.
// ConfigurationWriteBackLease определяет захват предложения исполнителем, барьером, попыткой,
// поколением и сроком. Каждый отчёт об эффекте проверяется относительно текущей аренды владельца, а не
// принимается по скопированным значениям.
type ConfigurationWriteBackLease struct {
	ProposalRef, Claimant, Fence string
	Attempt, ClaimGeneration     int64
	ExpiresAt                    time.Time
}

// ConfigurationWriteBackWork is the private integration-gateway envelope for executing or reconciling
// an exact Git effect. It includes definition and proposed bytes, credential descriptor, deterministic
// commit metadata, and candidate pins; it must not be returned as the public proposal view.
// ConfigurationWriteBackWork является закрытым заданием шлюза интеграций для исполнения или сверки
// точного Git-эффекта. Оно включает байты определения и предложения, дескриптор реквизитов,
// детерминированные метаданные коммита и привязки кандидата; его нельзя возвращать как публичное
// представление предложения.
type ConfigurationWriteBackWork struct {
	Lease                                                            ConfigurationWriteBackLease
	Proposal                                                         ConfigurationWriteBack
	Mode, Effect                                                     string
	DefinitionKey, DefinitionVersion, DefinitionDigest               string
	DefinitionPackage, ProposedContent                               []byte
	PublicConfiguration                                              map[string]any
	CredentialRevision                                               IntegrationCredentialRevision
	EffectMarker, CommitMessage, CommitAuthorName, CommitAuthorEmail string
	CommitTime                                                       time.Time
	CandidateTreeSHA, CandidateBlobSHA                               string
	EffectStartedAt                                                  *time.Time
	Deadline                                                         time.Time
}
