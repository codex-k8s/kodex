package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// RoleImagePlatform selects one exact operating system, architecture, and optional variant for a role
// image. Platform eligibility is checked by the build owner rather than inferred from these strings.
// RoleImagePlatform выбирает точные операционную систему, архитектуру и необязательный вариант образа
// роли. Допустимость платформы проверяет владелец сборки, а не сами строки.
type RoleImagePlatform struct {
	OS, Architecture, Variant string
}

// RoleImagePackage describes a typed package installation by manager, name, version, digest, and
// source reference. The builder consumes validated catalogue selections; this descriptor is not
// permission to run arbitrary package-manager commands.
// RoleImagePackage описывает типизированную установку пакета менеджером, именем, версией, дайджестом и
// ссылкой на источник. Сборщик использует проверенный выбор каталога; дескриптор не разрешает
// произвольные команды менеджера пакетов.
type RoleImagePackage struct {
	Manager, Name, Version, Digest, SourceRef string
}

// RoleImageTool describes an immutable tool input through its name, version, source, and expected
// SHA-256. Fetching and verification remain builder responsibilities; a descriptor alone does not
// establish trusted executable bytes.
// RoleImageTool описывает неизменяемый вход инструмента через имя, версию, источник и ожидаемый
// SHA-256. Получение и проверка остаются обязанностями сборщика; сам дескриптор не удостоверяет
// доверенные исполняемые байты.
type RoleImageTool struct {
	Name, Version, SourceRef, SHA256 string
}

// RoleImageRecipeInput groups the canonical build specification: base and source pins, context and
// toolchain digests, target platforms, packages, tools, and installation text. Secret values must not
// be placed here; the data structure itself does not validate or sanitize Dockerfile content.
// RoleImageRecipeInput объединяет каноническую спецификацию сборки: привязки основы и исходника,
// дайджесты контекста и инструментов, платформы, пакеты, инструменты и текст установки. Значения
// секретов сюда не помещаются; сама структура не проверяет и не очищает содержимое Dockerfile.
type RoleImageRecipeInput struct {
	BaseImageReference, BaseImageDigest, SourceRef, SourceRevision, SourceSHA256 string
	ContextRef, ContextSHA256, BuilderSHA256, FrontendSHA256, InstallationBlock  string
	ToolchainSHA256                                                              string
	EnvironmentKey                                                               string
	PackageKeys, ToolKeys                                                        []string
	Platforms                                                                    []RoleImagePlatform
	Packages                                                                     []RoleImagePackage
	Tools                                                                        []RoleImageTool
	Dockerfile                                                                   string
}

// RoleEnvironmentSelection carries owner-facing environment, package, and tool selections plus
// installation or Dockerfile input. The authoritative catalogue resolves supply-chain locators and
// validates supported customization instead of trusting caller-supplied build provenance.
// RoleEnvironmentSelection передаёт выбор окружения, пакетов и инструментов владельцем, а также
// установочный текст или Dockerfile. Авторитетный каталог определяет адреса цепочки поставки и
// проверяет допустимость настройки, а не доверяет происхождению сборки от вызывающего кода.
type RoleEnvironmentSelection struct {
	EnvironmentKey        string
	PackageKeys, ToolKeys []string
	InstallationBlock     string
	Dockerfile            string
}

// RoleImageRecipe presents the scoped recipe aggregate, its input, managed lineage, version and
// generation, policy and runtime-contract pins, and current artifact selection. NextActions and
// promoted references are read state, not authorization for a new build or promotion.
// RoleImageRecipe представляет агрегат рецепта в области владельца, вход, управляемое происхождение,
// версию и поколение, привязки политики и контракта исполнения и текущий выбор материала. NextActions
// и опубликованные ссылки являются состоянием чтения, а не разрешением новой сборки или публикации.
type RoleImageRecipe struct {
	ScopeKind, OrganizationRef                                       string
	SourceAvailable                                                  bool
	ManagedLineage                                                   *RoleImageManagedLineage
	Ref, ProjectRef, RoleDefinitionRef, Name, State                  string
	SpecSHA256, PolicySHA256, RoleRuntimeContractSHA256              string
	ActiveImageArtifactRef, PromotedImageReference                   string
	Version, Generation, PolicyRevision, RoleRuntimeContractRevision uint64
	Input                                                            RoleImageRecipeInput
	CreatedAt, UpdatedAt                                             time.Time
	NextActions                                                      []string
}

// ImageBuild records one scoped build attempt with immutable input and provenance digests, stage,
// diagnostics, claimant, fence, authority generation, and lease deadline. It carries a lease-token
// digest rather than the token; recorded progress does not imply admission of the resulting image.
// ImageBuild фиксирует попытку сборки в области владельца с дайджестами неизменяемого входа и
// происхождения, этапом, диагностикой, исполнителем, барьером, поколением полномочий и сроком аренды.
// Передаётся дайджест токена, а не токен; зафиксированный ход не означает допуска итогового образа.
type ImageBuild struct {
	ScopeKind, OrganizationRef, ProjectRef                                string
	SourceAvailable                                                       bool
	ConfigurationRevisionRef                                              string
	Ref, RecipeRef, SpecSHA256, Stage, StagingReference, ManifestDigest   string
	ProvenanceSHA256, ImmutableBuildSHA256, SafeErrorCode, DiagnosticCode string
	DiagnosticSummary, LeaseTokenSHA256, ClaimantWorkload                 string
	Version, RecipeVersion, RecipeGeneration, Fence, AuthorityGeneration  uint64
	Attempt, ProgressPercent                                              uint32
	LeaseExpiresAt                                                        *time.Time
	CreatedAt, UpdatedAt                                                  time.Time
	Dockerfile                                                            string
}

// RoleImageManagedLineage pins a recipe to its managed configuration revision, source, ownership mode,
// and origin. These values preserve provenance; they do not let UI writes overwrite Git-owned
// configuration without the owner's detach or copy transition.
// RoleImageManagedLineage закрепляет рецепт за ревизией управляемой конфигурации, источником, режимом
// владения и происхождением. Значения сохраняют происхождение и не разрешают интерфейсу перезаписывать
// Git-конфигурацию без перехода отключения или копирования владельцем.
type RoleImageManagedLineage struct {
	ConfigurationRef, RevisionRef, ManagedBy, SourceRef, SourceRevision, Origin string
	Revision                                                                    int64
}

// ImageArtifact aggregates exact build, source, policy, SBOM, vulnerability, admission, signature,
// promotion, platform, and tool-inventory evidence for one scoped image. Optional attempt and
// risk-decision projections describe stored evidence; the owner validates the complete pinned chain
// before admitting or promoting it.
// ImageArtifact объединяет точные свидетельства сборки, исходника, политики, SBOM, уязвимостей,
// допуска, подписи, публикации, платформ и инструментов одного образа в области владельца.
// Необязательные представления попытки и решения о риске описывают сохранённые свидетельства; до
// допуска или публикации владелец проверяет всю закреплённую цепочку.
type ImageArtifact struct {
	AdmissionAttempt                                                              *ImageAdmissionAttempt
	RiskDecision                                                                  *ImageAdmissionRiskDecision
	ScopeKind, OrganizationRef, ProjectRef                                        string
	Ref, RecipeRef, SpecSHA256, BuildRef, StagingReference, ManifestDigest        string
	ImmutableBuildSHA256, ProvenanceSHA256, BaseImageDigest, SourceSHA256         string
	ContextSHA256, BuilderSHA256, FrontendSHA256, ToolchainSHA256                 string
	PolicySHA256, SBOMSHA256, VulnerabilityEvidenceSHA256, AdmissionVerdict       string
	SignatureIdentity, SignatureSHA256, AdmissionReceiptSHA256                    string
	AdmissionReceiptOCIManifestDigest, PromotedReference, PromotionReadbackSHA256 string
	RoleRuntimeContractSHA256                                                     string
	PromotionState                                                                string
	PromotionRequested                                                            bool
	Version, RecipeVersion, RecipeGeneration, BuildVersion, PolicyRevision        uint64
	AdmissionRevision, RoleRuntimeContractRevision                                uint64
	BuildAttempt                                                                  uint32
	Platforms                                                                     []RoleImagePlatform
	DeclaredTools                                                                 []RoleImageTool
	ToolInventorySHA256                                                           string
	ToolInventory                                                                 *runtimecontract.ImageToolInventory
	PromotedAt                                                                    *time.Time
	CreatedAt, UpdatedAt                                                          time.Time
}

// RoleImageBuildInput is the immutable build-input snapshot with exact recipe, generation, policy,
// runtime-contract, source, context, and toolchain pins. It is distinct from the editable recipe
// selection and must remain bound to the claimed build attempt.
// RoleImageBuildInput является неизменяемым снимком входа сборки с точными привязками рецепта,
// поколения, политики, контракта исполнения, исходника, контекста и инструментов. Он отличается от
// редактируемого выбора рецепта и сохраняет привязку к захваченной попытке сборки.
type RoleImageBuildInput struct {
	ScopeKind, OrganizationRef, ProjectRef                                       string
	RecipeRef, SpecSHA256, BaseImageReference, BaseImageDigest                   string
	SourceRef, SourceRevision, SourceSHA256, ContextRef, ContextSHA256           string
	BuilderSHA256, FrontendSHA256, InstallationBlock, ToolchainSHA256            string
	PolicySHA256, ImmutableBuildSHA256, RoleRuntimeContractSHA256                string
	RecipeVersion, RecipeGeneration, PolicyRevision, RoleRuntimeContractRevision uint64
	Platforms                                                                    []RoleImagePlatform
	Packages                                                                     []RoleImagePackage
	Tools                                                                        []RoleImageTool
	Dockerfile                                                                   string
}

// ImageBuildClaim returns a build and input snapshot with the lease token, fence, authority
// generation, and expiry required by its worker. The token is an opaque capability, not safe log data;
// each effect still checks the owner's current claim.
// ImageBuildClaim возвращает сборку и снимок входа с необходимыми исполнителю токеном аренды,
// барьером, поколением полномочий и сроком. Токен является непрозрачной возможностью, а не безопасными
// данными лога; каждый эффект проверяет текущий захват владельца.
type ImageBuildClaim struct {
	Build               ImageBuild
	Input               RoleImageBuildInput
	LeaseToken          string
	Fence               uint64
	AuthorityGeneration uint64
	LeaseExpiresAt      time.Time
}

// ImageAdmissionClaim binds an admission worker to an exact artifact and attempt, claim token, fence,
// authority generation, deadline, and optional prior evidence and risk acceptance. Risk JSON and
// digests are pinned decision evidence, not a caller-issued waiver of admission checks.
// ImageAdmissionClaim связывает исполнителя допуска с точными материалом и попыткой, токеном захвата,
// барьером, поколением полномочий, сроком и необязательными прежними свидетельствами и принятием
// риска. JSON риска и дайджесты являются закреплёнными свидетельствами решения, а не выданным
// вызывающим кодом исключением из проверок допуска.
type ImageAdmissionClaim struct {
	Artifact                                                   ImageArtifact
	ClaimToken                                                 string
	Fence                                                      uint64
	AuthorityGeneration                                        uint64
	ClaimExpiresAt                                             time.Time
	AdmissionAttemptRef                                        string
	AdmissionAttempt                                           uint32
	RiskAcceptanceJSON, RiskAcceptanceSHA256                   string
	SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest string
	SourceAdmissionRevision                                    uint64
}

// RoleImageAdmissionFailure reports an owner-recorded technical admission outcome with exact recipe,
// build, attempt, generation, artifact, scope, and version pins. It is not a vulnerability verdict or
// permission to promote a failed image.
// RoleImageAdmissionFailure сообщает зафиксированный владельцем технический исход допуска с точными
// привязками рецепта, сборки, попытки, поколения, материала, области и версии. Это не заключение об
// уязвимостях и не разрешение публиковать образ с ошибкой.
type RoleImageAdmissionFailure struct {
	ImageArtifactRef, RecipeRef, BuildRef  string
	ScopeKind, OrganizationRef, ProjectRef string
	Version, RecipeGeneration              uint64
	BuildAttempt                           uint32
	State, ErrorCode                       string
}

// ImagePromotionClaim binds a promotion worker to the exact artifact, opaque claim, fence, authority
// generation, and deadline. Claim possession does not replace verification of admission evidence or
// the owner's current promotion state.
// ImagePromotionClaim связывает исполнителя публикации с точными материалом, непрозрачным захватом,
// барьером, поколением полномочий и сроком. Наличие захвата не заменяет проверку свидетельств допуска
// или текущего состояния публикации у владельца.
type ImagePromotionClaim struct {
	Artifact            ImageArtifact
	PromotionClaim      string
	Fence               uint64
	AuthorityGeneration uint64
	ClaimExpiresAt      time.Time
}

// ImagePromotionAuthorization pairs an artifact with a bounded-lifetime promotion authorization token.
// The token is a capability for the validated promotion path and must not be exposed through ordinary
// artifact views or logs.
// ImagePromotionAuthorization связывает материал с токеном разрешения публикации ограниченного срока.
// Токен является возможностью проверенного пути публикации и не раскрывается в обычных представлениях
// материала или логах.
type ImagePromotionAuthorization struct {
	Artifact               ImageArtifact
	AuthorizationToken     string
	AuthorizationExpiresAt time.Time
}

// RoleImageRecipeRevision captures one recipe revision's source, build, provenance, artifact,
// manifest, and promotion-receipt pins. Revision is distinct from the recipe aggregate version and
// generation; the snapshot is not a mutable latest-image reference.
// RoleImageRecipeRevision фиксирует привязки исходника, сборки, происхождения, материала, манифеста и
// квитанции публикации ревизии рецепта. Revision отличается от версии и поколения агрегата рецепта;
// снимок не является изменяемой ссылкой на последний образ.
type RoleImageRecipeRevision struct {
	Ref, RecipeRef, SpecSHA256, ProvenanceSHA256, SourceSHA256 string
	ImmutableBuildSHA256, ImageArtifactRef, ManifestDigest     string
	PromotedReference, PromotionReceiptSHA256                  string
	Revision, RecipeVersion, RecipeGeneration                  uint64
	CreatedAt                                                  time.Time
}

// RoleImagePromotionReceipt records the owner's promotion result for an exact recipe, artifact,
// provenance, and manifest with receipt digest and creation time. Replaying the receipt does not
// authorize publishing a different digest.
// RoleImagePromotionReceipt фиксирует результат публикации владельца для точных рецепта, материала,
// происхождения и манифеста с дайджестом квитанции и временем. Повторное чтение квитанции не разрешает
// публикацию другого дайджеста.
type RoleImagePromotionReceipt struct {
	Ref, RecipeRef, ImageArtifactRef, ProvenanceSHA256, ManifestDigest string
	ReceiptSHA256, State                                               string
	CreatedAt                                                          time.Time
}
