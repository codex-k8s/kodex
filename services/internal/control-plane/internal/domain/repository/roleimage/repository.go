// Package roleimage defines the transport-neutral repository port and exact inputs for recipe, build, admission and promotion operations.
// Package roleimage определяет независимый от транспорта порт репозитория и точные входы операций рецептов, сборки, допуска и продвижения.
package roleimage

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Filter selects recipe metadata by ownership scope, project or role, search text and state; Page carries the caller's pagination request, not an access grant.
// Filter отбирает метаданные рецептов по области владения, проекту или роли, поисковой строке и состоянию; Page задаёт пагинацию, а не право доступа.
type Filter struct {
	ScopeKind                     string
	ProjectRef, RoleDefinitionRef string
	Query, State                  string
	Page                          query.Page
}

// ManageInput carries a validated recipe action, mutation receipt parameters and the resolved principal together with recipe or build references. Scope and environment selection remain explicit so project and organization commands cannot be inferred from a recipe name.
// ManageInput передаёт проверенное действие над рецептом, параметры квитанции изменения и разрешённого субъекта вместе со ссылками на рецепт или сборку. Область и выбор окружения заданы явно: проектную либо организационную команду нельзя выводить из имени рецепта.
type ManageInput struct {
	ScopeKind               string
	Principal               value.Principal
	Mutation                value.Mutation
	Action                  string
	RecipeRef, ProjectRef   string
	BuildRef                string
	RoleDefinitionRef, Name string
	Recipe                  entity.RoleImageRecipeInput
	Environment             entity.RoleEnvironmentSelection
}

// ManageResult returns the authoritative recipe and any build or artifact produced by the action; Reused distinguishes replayed work. Nil optional results do not establish admission or promotion.
// ManageResult возвращает авторитетный рецепт и созданные действием сборку либо артефакт; Reused отличает повторное использование результата. Отсутствие необязательных результатов не подтверждает допуск или продвижение.
type ManageResult struct {
	Recipe   entity.RoleImageRecipe
	Build    *entity.ImageBuild
	Artifact *entity.ImageArtifact
	Reused   bool
}

// Detail groups a recipe's builds, active and candidate artifacts, and latest admission failure for an authorized read. Candidate availability is distinct from the artifact currently selected for use.
// Detail объединяет сборки рецепта, активный и кандидатный артефакты и последнюю ошибку допуска для разрешённого чтения. Наличие кандидата отличается от выбора артефакта для использования.
type Detail struct {
	Recipe             entity.RoleImageRecipe
	Builds             []entity.ImageBuild
	ActiveArtifact     *entity.ImageArtifact
	PromotionCandidate *entity.ImageArtifact
	AdmissionFailure   *entity.RoleImageAdmissionFailure
}

// BuildLeaseInput binds a build worker operation to its principal, idempotency key, lease token and expected version, fence and attempt. Carrying these fields does not itself validate the lease.
// BuildLeaseInput связывает операцию сборщика с субъектом, ключом идемпотентности, токеном аренды и ожидаемыми версией, барьером и попыткой. Само наличие полей ещё не проверяет аренду.
type BuildLeaseInput struct {
	Principal                            value.Principal
	IdempotencyKey, BuildRef, LeaseToken string
	ExpectedVersion, ExpectedFence       uint64
	ExpectedAttempt                      uint32
}

// BuildProgressInput adds a stage and nonterminal progress percentage to the exact build lease. Completion uses a separate operation rather than a progress value of one hundred.
// BuildProgressInput добавляет этап и процент незавершённого прогресса к точной аренде сборки. Завершение оформляется отдельной операцией, а не значением прогресса сто.
type BuildProgressInput struct {
	BuildLeaseInput
	Stage           string
	ProgressPercent uint32
}

// BuildCompletionInput identifies the staging image and manifest, provenance and immutable-input digests under the existing build lease. It requests build completion, not admission or registry promotion.
// BuildCompletionInput задаёт staging-образ и дайджесты манифеста, происхождения и неизменяемого входа в рамках существующей аренды. Это запрос завершения сборки, а не допуска либо продвижения в реестре.
type BuildCompletionInput struct {
	BuildLeaseInput
	StagingReference, ManifestDigest, ProvenanceSHA256, ImmutableBuildSHA256 string
}

// BuildFailureInput records a build error and bounded diagnostic code and summary against the leased attempt. It is separate from an image admission verdict.
// BuildFailureInput фиксирует ошибку сборки и ограниченные диагностические код и сводку для арендованной попытки. Эта ошибка отличается от заключения о допуске образа.
type BuildFailureInput struct {
	BuildLeaseInput
	ErrorCode, DiagnosticCode, DiagnosticSummary string
}

// AdmissionRecordInput binds a worker verdict to an exact admission attempt, artifact version and fence, image and evidence digests, policy revision, signature and receipt. Tool inventory and vulnerability projections accompany their own hashes; the record alone is not independent verification of those bytes.
// AdmissionRecordInput связывает заключение исполнителя с точной попыткой допуска, версией и барьером артефакта, дайджестами образа и доказательств, ревизией политики, подписью и квитанцией. Каталог инструментов и проекция уязвимостей сопровождаются своими хешами; сама структура не является независимой проверкой этих данных.
type AdmissionRecordInput struct {
	Principal                                                                                                       value.Principal
	IdempotencyKey, ArtifactRef, ClaimToken, ManifestDigest                                                         string
	ImmutableBuildSHA256, ProvenanceSHA256, SBOMSHA256                                                              string
	VulnerabilityEvidenceSHA256, PolicySHA256, Verdict                                                              string
	SignatureIdentity, SignatureSHA256, AdmissionReceiptSHA256                                                      string
	AdmissionReceiptOCIManifestDigest                                                                               string
	ToolInventoryJSON, ToolInventorySHA256                                                                          string
	ExpectedVersion, ExpectedFence, PolicyRevision                                                                  uint64
	ExpectedAdmissionAttemptRef, VulnerabilityReportJSON, VulnerabilityReportProjectionSHA256, RiskAcceptanceSHA256 string
	ExpectedAdmissionAttempt                                                                                        uint32
}

// VulnerabilityReportFilter scopes and paginates a stored artifact report, optionally selecting package, severity, advisory or blocking entries. ExpectedReportSHA256 pins the requested projection instead of trusting a matching package name.
// VulnerabilityReportFilter ограничивает область и страницы сохранённого отчёта артефакта, необязательно выбирая пакет, серьёзность, рекомендацию или блокирующие записи. ExpectedReportSHA256 закрепляет запрошенную проекцию вместо доверия к совпадению имени пакета.
type VulnerabilityReportFilter struct {
	ScopeKind, ProjectRef, RecipeRef, ArtifactRef               string
	PackageQuery, Severity, AdvisoryQuery, ExpectedReportSHA256 string
	BlockingOnly                                                *bool
	Page                                                        query.Page
}

// AdmissionRiskInput carries a human risk action and reason pinned to recipe, build, artifact and admission versions plus exact report and policy evidence. It is a request to the owning repository, not a caller-issued admission receipt.
// AdmissionRiskInput передаёт человеческое решение о риске и его причину, закреплённые за версиями рецепта, сборки, артефакта и допуска и точными доказательствами отчёта и политики. Это запрос владельцу данных, а не квитанция допуска, выданная вызывающим.
type AdmissionRiskInput struct {
	Principal                                                                                           value.Principal
	Mutation                                                                                            value.Mutation
	ScopeKind, ProjectRef, RecipeRef, ArtifactRef                                                       string
	ExpectedArtifactVersion, ExpectedAdmissionRevision, ExpectedRecipeVersion, ExpectedRecipeGeneration uint64
	ExpectedBuildRef                                                                                    string
	ExpectedBuildAttempt                                                                                uint32
	ManifestDigest, VulnerabilityEvidenceSHA256, ProjectionSHA256                                       string
	PriorAdmissionReceiptSHA256, PriorEvidenceManifestDigest                                            string
	PolicyRevision                                                                                      uint64
	PolicySHA256, Action, Reason                                                                        string
}

// AdmissionFailureInput identifies a failed worker attempt through its claim token, fence, authority generation and immutable image, build and policy bindings. ErrorCode reports technical failure, not a substitute accepted or rejected verdict.
// AdmissionFailureInput определяет неуспешную попытку исполнителя через токен захвата, барьер, поколение полномочий и неизменяемые привязки образа, сборки и политики. ErrorCode сообщает техническую ошибку, а не подменяет заключение о принятии или отклонении.
type AdmissionFailureInput struct {
	ExpectedAdmissionAttemptRef                                                                   string
	ExpectedAdmissionAttempt                                                                      uint32
	Principal                                                                                     value.Principal
	IdempotencyKey, ArtifactRef, ClaimToken                                                       string
	ManifestDigest, ImmutableBuildSHA256, ProvenanceSHA256, PolicySHA256                          string
	BuildRef, SpecSHA256, ErrorCode                                                               string
	ExpectedVersion, ExpectedFence, ExpectedAuthorityGeneration, PolicyRevision, RecipeGeneration uint64
	ExpectedBuildAttempt                                                                          uint32
}

// AdmissionExpiryInput pins the attempt and immutable dependencies for owner-side expiry without accepting a worker token or caller-chosen failure reason. The owner determines whether time permits expiry.
// AdmissionExpiryInput закрепляет попытку и неизменяемые зависимости для завершения по сроку на стороне владельца, не принимая токен исполнителя или выбранную вызывающим причину ошибки. Возможность завершения по времени определяет владелец.
type AdmissionExpiryInput struct {
	ExpectedAdmissionAttemptRef                                                                   string
	ExpectedAdmissionAttempt                                                                      uint32
	Principal                                                                                     value.Principal
	IdempotencyKey, ArtifactRef, ManifestDigest, ImmutableBuildSHA256, ProvenanceSHA256           string
	PolicySHA256, BuildRef, SpecSHA256                                                            string
	ExpectedVersion, ExpectedFence, ExpectedAuthorityGeneration, PolicyRevision, RecipeGeneration uint64
	ExpectedBuildAttempt                                                                          uint32
}

// PromotionAuthorizeInput requests authorization for one claimed artifact version and manifest using the promotion claim and idempotency key. It does not contain a caller-selected promoted destination.
// PromotionAuthorizeInput запрашивает разрешение для одной захваченной версии артефакта и манифеста с использованием захвата продвижения и ключа идемпотентности. Структура не содержит выбранного вызывающим конечного адреса образа.
type PromotionAuthorizeInput struct {
	Principal                                                   value.Principal
	IdempotencyKey, ArtifactRef, PromotionClaim, ManifestDigest string
	ExpectedVersion                                             uint64
}

// AdmissionTerminalInput reads the outcome of the original claim with exact expiry-style bindings and source admission evidence. ClaimIdempotencyKey resolves the owner's receipt; this read does not renew or reacquire the claim.
// AdmissionTerminalInput читает исход первоначального захвата с точными привязками завершения по сроку и исходными доказательствами допуска. ClaimIdempotencyKey разрешает квитанцию владельца; чтение не продлевает и не получает захват заново.
type AdmissionTerminalInput struct {
	AdmissionExpiryInput
	ClaimIdempotencyKey                                                              string
	RiskAcceptanceSHA256, SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest string
	SourceAdmissionRevision                                                          uint64
}

// AdmissionTerminalProof returns the claimed artifact snapshot and claim identity alongside the observed terminal attempt, fence and artifact versions. Source receipt and risk evidence remain explicit so recovery can compare the original lineage.
// AdmissionTerminalProof возвращает снимок захваченного артефакта и идентичность захвата вместе с наблюдаемыми конечными версиями попытки, барьера и артефакта. Исходная квитанция и доказательства решения о риске заданы явно для сопоставления происхождения при восстановлении.
type AdmissionTerminalProof struct {
	RiskAcceptanceSHA256, SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest string
	SourceAdmissionRevision                                                          uint64
	State                                                                            string
	ClaimedArtifact                                                                  entity.ImageArtifact
	AttemptRef                                                                       string
	Attempt                                                                          uint32
	ClaimFence, ClaimAuthorityGeneration                                             uint64
	TerminalArtifactVersion, TerminalFence, TerminalAttemptVersion                   uint64
}

// PromotionCompleteInput reports the promoted reference, manifest and readback digest for the authorized artifact version. AuthorizationToken is consumed through the owner boundary rather than treated as proof that a push succeeded.
// PromotionCompleteInput сообщает конечную ссылку образа, манифест и дайджест контрольного чтения для разрешённой версии артефакта. AuthorizationToken обрабатывается через границу владельца и сам по себе не доказывает успешную отправку в реестр.
type PromotionCompleteInput struct {
	Principal                                                  value.Principal
	IdempotencyKey, ArtifactRef, AuthorizationToken            string
	PromotedReference, ManifestDigest, PromotionReadbackSHA256 string
	ExpectedVersion                                            uint64
}

// PromotionRequestInput carries a recipe and candidate artifact with expected provenance and mutation preconditions. ScopeKind keeps organization promotion separate from project promotion; the repository resolves ownership.
// PromotionRequestInput передаёт рецепт и кандидатный артефакт с ожидаемым происхождением и предусловиями изменения. ScopeKind разделяет организационное и проектное продвижение; владение разрешает репозиторий.
type PromotionRequestInput struct {
	ScopeKind                                        string
	Principal                                        value.Principal
	Mutation                                         value.Mutation
	RecipeRef, ArtifactRef, ExpectedProvenanceSHA256 string
}

// SupplyWorkAvailability exposes separate admission and promotion work hints. A true flag is not a reservation; workers must still claim the corresponding task.
// SupplyWorkAvailability предоставляет раздельные признаки наличия работы по допуску и продвижению. Значение true не является резервированием: исполнитель ещё должен захватить соответствующую задачу.
type SupplyWorkAvailability struct {
	AdmissionAvailable bool
	PromotionAvailable bool
}

// Repository is the transport-neutral port for scoped recipe reads and mutations and fenced build, admission and promotion operations. Implementations own persistence and claim state; DTO fields and idempotency keys do not replace principal and resource checks.
// Repository — независимый от транспорта порт чтения и изменения рецептов в своей области и операций сборки, допуска и продвижения с барьерами. Реализация владеет хранением и состоянием захватов; поля DTO и ключи идемпотентности не заменяют проверки субъекта и ресурса.
type Repository interface {
	// GetVulnerabilityReport reads the authorized, version-pinned vulnerability projection with entry filters and pagination; it does not change the admission decision.
	// GetVulnerabilityReport читает разрешённую проекцию уязвимостей с закреплением версии, фильтрами записей и пагинацией; решение о допуске не меняется.
	GetVulnerabilityReport(context.Context, value.Principal, VulnerabilityReportFilter) (entity.ImageVulnerabilityReport, error)
	// DecideAdmissionRisk persists the supplied human risk decision against the exact artifact and evidence preconditions, returning the resulting admission-risk record.
	// DecideAdmissionRisk сохраняет переданное человеческое решение о риске при точных предусловиях артефакта и доказательств и возвращает результат решения.
	DecideAdmissionRisk(context.Context, AdmissionRiskInput) (entity.ImageAdmissionRiskResult, error)
	// ListOrganizationRevisions pages immutable revisions of one organization recipe under principal authorization and returns the continuation cursor.
	// ListOrganizationRevisions постранично читает неизменяемые ревизии одного организационного рецепта с авторизацией субъекта и возвращает курсор продолжения.
	ListOrganizationRevisions(context.Context, value.Principal, string, query.Page) ([]entity.RoleImageRecipeRevision, string, error)
	// ResolvePrincipal resolves the validated transport principal into current authoritative scope for subsequent operations; resource references are not a source of identity.
	// ResolvePrincipal разрешает проверенного транспортного субъекта в актуальную авторитетную область для последующих операций; ссылки ресурсов не являются источником идентичности.
	ResolvePrincipal(context.Context, value.Principal) (value.Principal, error)
	// List pages project-scoped recipes and returns both a continuation cursor and total count while enforcing the principal's resource boundary.
	// List постранично читает проектные рецепты и возвращает курсор продолжения и общее число с соблюдением границы ресурсов субъекта.
	List(context.Context, value.Principal, Filter) ([]entity.RoleImageRecipe, string, int64, error)
	// Get reads the authorized project recipe detail, including related builds and artifacts; a matching reference alone does not establish access.
	// Get читает разрешённые подробности проектного рецепта, включая связанные сборки и артефакты; совпадение ссылки само по себе не даёт доступа.
	Get(context.Context, value.Principal, string) (Detail, error)
	// Manage executes a project recipe action under the supplied mutation preconditions and returns its recipe, optional build and artifact, and replay indicator.
	// Manage исполняет действие над проектным рецептом с переданными предусловиями изменения и возвращает рецепт, необязательные сборку и артефакт и признак повтора.
	Manage(context.Context, ManageInput) (ManageResult, error)
	// ListOrganization pages organization-scoped recipes separately from project recipes, returning cursor and total count under organization access rules.
	// ListOrganization читает страницы организационных рецептов отдельно от проектных, возвращая курсор и общее число согласно правилам организационного доступа.
	ListOrganization(context.Context, value.Principal, Filter) ([]entity.RoleImageRecipe, string, int64, error)
	// GetOrganization reads the detail of an organization-owned recipe through its own scope checks rather than accepting a project recipe as equivalent.
	// GetOrganization читает подробности принадлежащего организации рецепта с собственными проверками области, не подменяя его проектным рецептом.
	GetOrganization(context.Context, value.Principal, string) (Detail, error)
	// ManageOrganization applies an organization recipe action; project and role references are not alternative authority for this path.
	// ManageOrganization применяет действие над организационным рецептом; ссылки проекта и роли не являются альтернативными полномочиями для этого пути.
	ManageOrganization(context.Context, ManageInput) (ManageResult, error)
	// RequestOrganizationPromotion records a promotion request for an organization recipe using its mutation and provenance bindings; worker claim and completion remain separate.
	// RequestOrganizationPromotion фиксирует запрос продвижения организационного рецепта с привязками изменения и происхождения; захват и завершение исполнителем остаются отдельными операциями.
	RequestOrganizationPromotion(context.Context, PromotionRequestInput) (entity.RoleImagePromotionReceipt, error)
	// ClaimBuild claims available build work for the authorized principal using the idempotency key and returns the fenced attempt and lease data.
	// ClaimBuild захватывает доступную сборку для разрешённого субъекта с ключом идемпотентности и возвращает попытку с барьером и данными аренды.
	ClaimBuild(context.Context, value.Principal, string) (entity.ImageBuildClaim, error)
	// RenewBuild renews the exact claimed build attempt using token, version and fence preconditions and returns the updated lease.
	// RenewBuild продлевает точную захваченную попытку сборки с предусловиями токена, версии и барьера и возвращает обновлённую аренду.
	RenewBuild(context.Context, BuildLeaseInput) (entity.ImageBuildClaim, error)
	// ReportBuildProgress records a nonterminal stage and percentage for the fenced build lease; it does not publish an artifact.
	// ReportBuildProgress записывает незавершённый этап и процент для аренды сборки с барьером; артефакт не публикуется.
	ReportBuildProgress(context.Context, BuildProgressInput) (entity.ImageBuild, error)
	// CompleteBuild finishes the leased build and returns both the build and staging artifact linked to its immutable evidence; admission remains a later step.
	// CompleteBuild завершает арендованную сборку и возвращает сборку и staging-артефакт, связанные с неизменяемыми доказательствами; допуск выполняется позднее.
	CompleteBuild(context.Context, BuildCompletionInput) (entity.ImageBuild, entity.ImageArtifact, error)
	// FailBuild records technical failure and diagnostics for the exact leased attempt without creating an accepted image artifact.
	// FailBuild фиксирует техническую ошибку и диагностику точной арендованной попытки, не создавая допущенный артефакт образа.
	FailBuild(context.Context, BuildFailureInput) (entity.ImageBuild, error)
	// GetSupplyWorkAvailability reads admission and promotion work hints for the authorized controller without claiming either queue.
	// GetSupplyWorkAvailability читает признаки работы по допуску и продвижению для разрешённого контроллера, не захватывая ни одну из очередей.
	GetSupplyWorkAvailability(context.Context, value.Principal) (SupplyWorkAvailability, error)
	// ClaimAdmission reserves an artifact admission attempt with the idempotency key and returns its worker claim and pinned evidence.
	// ClaimAdmission резервирует попытку допуска артефакта с ключом идемпотентности и возвращает захват исполнителя и закреплённые доказательства.
	ClaimAdmission(context.Context, value.Principal, string) (entity.ImageAdmissionClaim, error)
	// RecordAdmission persists the worker verdict, report and inventory evidence against the exact claim, artifact and policy bindings, returning the updated artifact.
	// RecordAdmission сохраняет заключение исполнителя и доказательства отчёта и каталога инструментов при точных привязках захвата, артефакта и политики и возвращает обновлённый артефакт.
	RecordAdmission(context.Context, AdmissionRecordInput) (entity.ImageArtifact, error)
	// FailAdmission terminates a matching worker attempt with a technical failure record; a failed check is not an accepted verdict.
	// FailAdmission завершает соответствующую попытку исполнителя записью технической ошибки; неуспешная проверка не является положительным допуском.
	FailAdmission(context.Context, AdmissionFailureInput) (entity.RoleImageAdmissionFailure, error)
	// ExpireAdmission asks the owner to expire the pinned attempt using authoritative time and dependencies, without accepting a worker failure token or reason.
	// ExpireAdmission запрашивает у владельца завершение закреплённой попытки по авторитетному времени и зависимостям, не принимая токен или причину ошибки исполнителя.
	ExpireAdmission(context.Context, AdmissionExpiryInput) (entity.RoleImageAdmissionFailure, error)
	// GetAdmissionTerminal reads the original claim's terminal outcome with exact attempt and evidence bindings; it does not acquire or renew work.
	// GetAdmissionTerminal читает конечный исход первоначального захвата с точными привязками попытки и доказательств; захвата или продления работы нет.
	GetAdmissionTerminal(context.Context, AdmissionTerminalInput) (AdmissionTerminalProof, error)
	// GetAdmissionRecoveryTerminal resolves a claim receipt by its original key for the authorized recovery controller and reads the terminal outcome without a new claim.
	// GetAdmissionRecoveryTerminal разрешает квитанцию по исходному ключу захвата для разрешённого контроллера восстановления и читает конечный исход без нового захвата.
	GetAdmissionRecoveryTerminal(context.Context, value.Principal, string) (AdmissionTerminalProof, error)
	// ClaimPromotion claims requested promotion work for the authorized worker and returns the artifact, evidence and promotion claim.
	// ClaimPromotion захватывает запрошенное продвижение для разрешённого исполнителя и возвращает артефакт, доказательства и захват продвижения.
	ClaimPromotion(context.Context, value.Principal, string) (entity.ImagePromotionClaim, error)
	// RequestPromotion records project-scoped promotion intent with mutation and expected provenance preconditions; it does not itself copy image bytes.
	// RequestPromotion фиксирует проектное намерение продвижения с предусловиями изменения и ожидаемого происхождения; сами байты образа не копируются.
	RequestPromotion(context.Context, PromotionRequestInput) (entity.RoleImagePromotionReceipt, error)
	// AuthorizePromotion verifies the claimed artifact version and manifest through the owner and returns the authorization used by the promotion worker.
	// AuthorizePromotion проверяет захваченную версию артефакта и манифест через владельца и возвращает разрешение для исполнителя продвижения.
	AuthorizePromotion(context.Context, PromotionAuthorizeInput) (entity.ImagePromotionAuthorization, error)
	// CompletePromotion records the authorized promoted reference and exact manifest and readback digest, returning the final artifact state.
	// CompletePromotion фиксирует разрешённую конечную ссылку и точные манифест и дайджест контрольного чтения, возвращая конечное состояние артефакта.
	CompletePromotion(context.Context, PromotionCompleteInput) (entity.ImageArtifact, error)
}
