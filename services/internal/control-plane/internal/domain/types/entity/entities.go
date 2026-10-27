// Package entity declares control-plane domain records and owner-produced views.
// Shapes and field names do not authenticate callers, enforce lifecycle or make
// nested maps and slices immutable; those guarantees belong to domain boundaries.
// Пакет entity объявляет записи домена control-plane и представления владельца.
// Формы и имена полей не аутентифицируют стороны, не обеспечивают жизненный цикл
// и не делают вложенные maps и slices неизменяемыми: это обязанности доменных границ.
package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Project carries the domain's project identity/version, lifecycle and trash deadlines together with
// activity/counter and next-action views; these fields neither perform purge nor grant project access.
// Project несёт доменные личность и версию проекта, жизненный цикл и сроки корзины вместе с
// активностью, счётчиками и следующими действиями; поля не выполняют очистку и не дают доступ к
// проекту.
type Project struct {
	LastActivityAt                          *time.Time
	DeletedAt, PurgeAfter                   *time.Time
	IntegrationState                        string
	Ref, Name, Purpose, Language, Lifecycle string
	Version                                 int64
	AgentCount, WorkflowCount               int32
	ActiveRunCount, PendingGateCount        int32
	CreatedAt, UpdatedAt                    time.Time
	NextActions                             []string
}

// SearchResult is a compact cross-kind hit with exact resource/project references, display text, state
// and update time, not a full resource or authorization decision.
// SearchResult — краткая находка разных видов с точными ссылками ресурса и проекта, текстом,
// состоянием и временем изменения, не полный ресурс и не решение о доступе.
type SearchResult struct {
	Kind, Ref, ProjectRef, Title, Subtitle, State string
	UpdatedAt                                     time.Time
}

// VFSNode describes a logical filesystem entry with resource/run provenance, revision/digest,
// lifecycle/scan state and selection metadata. Path and Selectable are data; they do not authorize
// host filesystem access or bypass admission.
// VFSNode описывает запись логической файловой системы с происхождением ресурса и запуска, ревизией,
// хешем, жизненным циклом, сканированием и выбором. Path и Selectable — данные, не разрешение доступа
// к файлам хоста или обхода допуска.
type VFSNode struct {
	Ref, Path, ParentPath, Name, Kind, ProjectRef, EntityRef, RunRef, Digest string
	Directory                                                                bool
	SizeBytes                                                                int64
	ModifiedAt                                                               time.Time
	Version, Revision                                                        int64
	RevisionRef, LifecycleState, ScanState, ResourceKind, SelectionReason    string
	Selectable                                                               bool
	NextActions                                                              []string
}

// User provides the lightweight identity view used in memberships, including display name,
// masked-email field and active flag; the struct does not itself mask or authenticate an email value.
// User задаёт краткое представление личности для членства: имя, поле скрытого email и активность; сама
// структура email не скрывает и не аутентифицирует.
type User struct {
	Ref, DisplayName, EmailMasked string
	Active                        bool
}

// Membership joins one project role and user with versioned activity, permission and next-action
// projections; constructing this value does not create membership or assign its permissions.
// Membership соединяет роль проекта и пользователя с версионными представлениями активности, прав и
// действий; создание значения не создаёт членство и не выдаёт права.
type Membership struct {
	Ref, ProjectRef, Role string
	User                  User
	Permissions           []string
	NextActions           []string
	Active                bool
	Version               int64
}

// InstructionVersion groups instruction source/digest/parent and numbered draft/publication metadata,
// including core ownership and validation problems; content presence is not proof of publication or
// approval.
// InstructionVersion объединяет исходник, хеш, родителя и номер версии инструкций с данными черновика
// и публикации, базовым владением и замечаниями проверки; наличие текста не доказывает публикацию или
// одобрение.
type InstructionVersion struct {
	Ref, State, Content, Digest, ParentRef string
	VersionNumber                          int32
	Core                                   bool
	ValidationProblems                     []string
	CreatedAt                              time.Time
	PublishedAt                            *time.Time
}

// RuntimeSelection is the selectable runtime/provider/model identity and revision/readiness view;
// Ready is an observed field rather than a provider call or reservation.
// RuntimeSelection — представление выбираемых runtime, провайдера, модели, ревизии и готовности; Ready
// является наблюдаемым полем, не вызовом провайдера или резервированием.
type RuntimeSelection struct {
	Ref, Name, Provider, Model, RuntimeRevision string
	Ready                                       bool
}

// ProviderAccountCandidate pins a weighted account choice to catalog/model-capability provenance and
// optional provider/default-reasoning metadata; neither weight nor reference proves current account
// eligibility.
// ProviderAccountCandidate закрепляет взвешенный выбор аккаунта с происхождением каталога и
// возможностей модели и необязательными данными провайдера и reasoning; вес и ссылка не доказывают
// текущую допустимость аккаунта.
type ProviderAccountCandidate struct {
	AccountRef             string `json:"accountRef"`
	Weight                 int32  `json:"weight"`
	CatalogRevision        string `json:"catalogRevision,omitempty"`
	CatalogDigest          string `json:"catalogDigest,omitempty"`
	ModelCapabilityDigest  string `json:"modelCapabilityDigest,omitempty"`
	ProviderDefinitionKey  string `json:"providerDefinitionKey,omitempty"`
	DefaultReasoningEffort string `json:"defaultReasoningEffort,omitempty"`
}

// ProviderAccountPolicyVersion carries a numbered, digested routing-policy snapshot and its ordered
// account candidates, not mutable provider credentials or an execution claim.
// ProviderAccountPolicyVersion несёт нумерованный снимок политики маршрутизации с хешем и
// упорядоченными кандидатами аккаунтов, не изменяемые credentials или захват выполнения.
type ProviderAccountPolicyVersion struct {
	Ref, Mode, Digest string
	Version           int64
	AccountCandidates []ProviderAccountCandidate
	CreatedAt         time.Time
}

// AgentRuntimeConfiguration binds one agent/profile/provider/model selection and provider-policy
// snapshot to an exact configuration version/digest; validation and activation occur outside this
// record.
// AgentRuntimeConfiguration связывает выбор агента, профиля, провайдера и модели и снимок политики
// аккаунтов с точными версией и хешем конфигурации; проверка и активация выполняются вне записи.
type AgentRuntimeConfiguration struct {
	Ref, AgentRef, RuntimeProfileRef, Provider, Model, Digest string
	Version                                                   int64
	ProviderPolicy                                            ProviderAccountPolicyVersion
	CreatedAt                                                 time.Time
}

// ConfigOverlayVersion preserves overlay source, numbered revision/digest, publication times and both
// string messages and structured schema-bound diagnostics; the fields do not parse or validate the
// overlay.
// ConfigOverlayVersion сохраняет исходник overlay, номер ревизии, хеш, сроки публикации и строковые
// сообщения со структурированной диагностикой, привязанной к схеме; поля overlay не разбирают и не
// проверяют.
type ConfigOverlayVersion struct {
	Ref, State, Content, Digest  string
	Version, Revision            int64
	ValidationMessages           []string
	CreatedAt                    time.Time
	PublishedAt                  *time.Time
	Diagnostics                  []runtimecontract.ConfigOverlayDiagnostic
	SchemaRevision, SchemaDigest string
}

// RuntimeEnvironmentValue is one named runtime environment value encoded as name/value JSON; this
// shape does not enforce that a caller has supplied nonsecret data.
// RuntimeEnvironmentValue — одно именованное значение окружения runtime в JSON name/value; форма не
// обеспечивает, что вызывающий передал несекретные данные.
type RuntimeEnvironmentValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// RuntimeSecretDescriptor pins a named runtime secret to its logical revision and exact
// namespace/object UID/resource-version/key/content digest. It contains locators and provenance, not
// the secret bytes, and is not itself a read grant.
// RuntimeSecretDescriptor закрепляет именованный runtime-секрет с логической ревизией и точными
// namespace, UID, resource version, ключом и хешем содержимого. Несёт указатели и происхождение, не
// байты секрета, и сам разрешением чтения не является.
type RuntimeSecretDescriptor struct {
	Name                  string `json:"name"`
	SecretRef             string `json:"secret_ref"`
	Namespace             string `json:"namespace"`
	Revision              int64  `json:"revision"`
	SecretName            string `json:"secret_name"`
	SecretKey             string `json:"secret_key"`
	SecretUID             string `json:"secret_uid"`
	SecretResourceVersion string `json:"secret_resource_version"`
	ContentSHA256         string `json:"content_sha256"`
}

// RuntimeSecretBinding names a logical secret revision under an environment key before materialized
// object descriptors are resolved; it does not carry credentials.
// RuntimeSecretBinding называет логическую ревизию секрета под ключом окружения до разрешения
// материализованных дескрипторов объектов; credentials не содержит.
type RuntimeSecretBinding struct {
	Name      string
	SecretRef string
	Revision  int64
}

// RuntimeEnvironmentTool describes an advertised tool label, command, help and optional usage hint;
// declaration alone does not verify a binary or permit arbitrary execution.
// RuntimeEnvironmentTool описывает имя инструмента, команду, справку и необязательную подсказку
// использования; декларация сама бинарный файл не проверяет и произвольное выполнение не разрешает.
type RuntimeEnvironmentTool struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
	UsageHint   string `json:"usage_hint,omitempty"`
}

// PromptVariableCatalog pages available template-variable metadata with total/cursor and the exact
// prompt context pin to which availability was computed.
// PromptVariableCatalog возвращает страницу метаданных переменных шаблона с количеством, курсором и
// точной привязкой контекста, для которого рассчитана доступность.
type PromptVariableCatalog struct {
	Variables     []TemplateVariable
	Total         int64
	NextPageToken string
	ContextPin    PromptContextPin
}

// RuntimeEnvironmentImage pins an environment to artifact/recipe, immutable image reference/digest and
// recipe generation; runtime-contract and platform-bootstrap flags are internal-only JSON-excluded
// fields, not user-selected admission.
// RuntimeEnvironmentImage закрепляет окружение с артефактом, рецептом, неизменяемыми ссылкой и хешем
// образа и поколением рецепта; runtime-contract и platform-bootstrap поля исключены из JSON и не
// означают пользовательский допуск.
type RuntimeEnvironmentImage struct {
	ArtifactRef                 string `json:"artifact_ref"`
	RecipeRef                   string `json:"recipe_ref"`
	Reference                   string `json:"reference"`
	Digest                      string `json:"digest"`
	RecipeGeneration            int64  `json:"recipe_generation"`
	RoleRuntimeContractSHA256   string `json:"-"`
	RoleRuntimeContractRevision int64  `json:"-"`
	PlatformOwnedBootstrap      bool   `json:"-"`
}

// RuntimeEnvironmentVersion groups a versioned/digested image, tools, values, exact secret descriptors
// and runtime policy snapshot. Its slices are ordinary mutable Go data, not automatically immutable or
// deep-copied.
// RuntimeEnvironmentVersion объединяет версионный снимок образа, инструментов, значений, точных
// дескрипторов секретов и runtime-политики с хешем. Slices остаются обычными изменяемыми Go-данными,
// не становятся неизменяемыми и не копируются глубоко автоматически.
type RuntimeEnvironmentVersion struct {
	Ref, Digest       string
	Version, Revision int64
	Image             RuntimeEnvironmentImage
	Tools             []RuntimeEnvironmentTool
	Values            []RuntimeEnvironmentValue
	SecretDescriptors []RuntimeSecretDescriptor
	Policy            runtimecontract.RuntimeEnvironmentPolicy
	CreatedAt         time.Time
}

// RuntimeEnvironmentSet carries scope/organization/project identity and the currently published
// environment version together with readiness blockers/actions; it does not choose scope from a
// request or publish a version.
// RuntimeEnvironmentSet несёт область, организацию, проект и текущую опубликованную версию окружения
// вместе с препятствиями готовности и действиями; область из запроса не выбирает и версию не
// публикует.
type RuntimeEnvironmentSet struct {
	ScopeKind, OrganizationRef                string
	Ref, ProjectRef, Name, Description, State string
	Version                                   int64
	CurrentVersion                            RuntimeEnvironmentVersion
	UpdatedAt                                 time.Time
	Ready                                     bool
	ReadinessBlockers, NextActions            []string
}

// RuntimeEnvironmentReadiness records readiness/blockers for one environment against an exact
// published version/digest and observation time; this view does not independently validate provenance
// or enforce publication.
// RuntimeEnvironmentReadiness фиксирует готовность и препятствия окружения относительно точных
// опубликованных версии, хеша и времени наблюдения; view происхождение независимо не проверяет и
// публикацию не обеспечивает.
type RuntimeEnvironmentReadiness struct {
	EnvironmentRef, PublishedVersionRef, PublishedVersionDigest string
	EnvironmentVersion                                          int64
	Ready                                                       bool
	Blockers                                                    []string
	ObservedAt                                                  time.Time
}

// AgentRuntimeEnvironmentBinding identifies a versioned agent-to-environment binding with the exact
// environment version reference/digest; changing the struct does not activate a binding.
// AgentRuntimeEnvironmentBinding определяет версионную связь агента с окружением и точными ссылкой
// версии и хешем; изменение структуры связь не активирует.
type AgentRuntimeEnvironmentBinding struct {
	Ref, AgentRef, EnvironmentRef, Digest, VersionRef string
	Version                                           int64
}

// AgentRuntimeConfigurationView composes configuration, published/optional draft overlay,
// schema/context bindings and effective environment with a safe configuration display. It is a view,
// not permission to log nested source or bypass owner updates.
// AgentRuntimeConfigurationView соединяет конфигурацию, опубликованный и необязательный черновой
// overlay, схему, контекстные связи и действующее окружение с безопасным представлением настроек. Это
// view, не разрешение журналировать вложенные исходники или обходить изменения владельца.
type AgentRuntimeConfigurationView struct {
	OverlaySchema                 runtimecontract.ConfigOverlaySchema
	SkillBindings, MemoryBindings []AgentContextBinding
	Configuration                 AgentRuntimeConfiguration
	PublishedOverlay              ConfigOverlayVersion
	DraftOverlay                  *ConfigOverlayVersion
	EnvironmentBinding            AgentRuntimeEnvironmentBinding
	Environment                   RuntimeEnvironmentSet
	SafeEffectiveConfig           string
	AgentVersion                  int64
}

// TemplateVariable describes availability/reason, source/type/examples and optional
// collection/item-field metadata for one prompt variable; it contains no resolved variable value.
// TemplateVariable описывает доступность, причину, источник, тип, примеры и необязательные сведения
// коллекции и полей элемента одной переменной промпта; разрешённого значения переменной не содержит.
type TemplateVariable struct {
	Available                                bool
	Reason                                   string
	Name, Type, Description, Example, Source string
	Collection                               bool
	ItemType, RangeExample                   string
	ItemFields                               []TemplateVariableField
}

// TemplateVariableField names and describes the type of one collection-item field; it is schema
// metadata rather than a materialized item.
// TemplateVariableField называет и описывает тип поля элемента коллекции; это метаданные схемы, не
// материализованный элемент.
type TemplateVariableField struct{ Name, Type, Description string }

// EmailMailboxCredential binds a named credential kind/connection and generation/version to an exact
// secret revision/object digest; these descriptors are not raw mailbox passwords or mail-operation
// authority.
// EmailMailboxCredential связывает имя, вид credential, подключение, поколение и версию с точной
// ревизией секрета и хешем объекта; дескрипторы не являются паролями ящика или полномочиями почтовой
// операции.
type EmailMailboxCredential struct {
	Name, Kind, ConnectionRef                                  string
	Generation, ConnectionVersion                              int64
	ContentSHA256, SecretRef, SecretUID, SecretResourceVersion string
}

// ProviderDefinition describes a provider's advertised authorization methods, model catalog and
// availability/readiness blockers; it carries no authorization secret or guarantee that an account can
// execute now.
// ProviderDefinition описывает предлагаемые способы авторизации, каталог моделей и доступность
// провайдера с препятствиями готовности; секрета авторизации и гарантии выполнения аккаунтом сейчас не
// содержит.
type ProviderDefinition struct {
	Key, Name, Description, DefaultModelID string
	AuthorizationMethods, ModelIDs         []string
	Models                                 []ModelCapability
	Available, Ready                       bool
	ReadinessBlockers                      []string
}

// ModelCapability ties one model/provider identity to reasoning options/defaults, eligible account
// references and readiness/default flags; it does not reserve account capacity.
// ModelCapability связывает модель и провайдера с вариантами reasoning, значениями по умолчанию,
// допустимыми аккаунтами и признаками готовности и defaults; ёмкость аккаунта не резервирует.
type ModelCapability struct {
	ID, ProviderDefinitionKey, DefaultReasoningEffort string
	ReasoningEfforts                                  []string
	EligibleProviderAccountRefs, ReadinessBlockers    []string
	Available, IsDefault                              bool
}

// ModelCatalog is a paginated, revision/digest-tagged model view with optional source-status metadata;
// page contents alone do not prove catalog freshness.
// ModelCatalog — постраничное представление моделей с ревизией, хешем и необязательным состоянием
// источника; сами элементы страницы свежесть каталога не доказывают.
type ModelCatalog struct {
	Models                          []ModelCapability
	Total                           int64
	NextPageToken, Revision, Digest string
	Status                          *ModelCatalogStatus
}

// ModelCatalogStatus describes source state/failure and optional observed/expiry times without
// fetching or refreshing the external catalog.
// ModelCatalogStatus описывает состояние и ошибку источника и необязательные сроки наблюдения и
// истечения, не загружая и не обновляя внешний каталог.
type ModelCatalogStatus struct {
	State, Source, Failure string
	ObservedAt, ExpiresAt  *time.Time
}

// RuntimeWorkspacePathRule is one path/access policy declaration; it neither canonicalizes the path
// nor opens or grants access to a host file.
// RuntimeWorkspacePathRule — декларация пути и доступа в политике; путь не нормализует, файл хоста не
// открывает и доступ не выдаёт.
type RuntimeWorkspacePathRule struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

// RuntimeWorkspacePolicy pins workspace root and ordered path rules with revision/digest,
// writable-byte/file limits and denial reasons; enforcement and path-boundary validation remain
// runtime responsibilities.
// RuntimeWorkspacePolicy закрепляет корень workspace и правила путей с ревизией, хешем, пределами
// записываемых байтов и файлов и причинами отказа; исполнение и проверка границ путей остаются
// обязанностями runtime.
type RuntimeWorkspacePolicy struct {
	Revision             int64                      `json:"revision"`
	Root                 string                     `json:"root"`
	Digest               string                     `json:"digest"`
	Rules                []RuntimeWorkspacePathRule `json:"rules"`
	MaximumWritableBytes int64                      `json:"maximumWritableBytes"`
	MaximumFileCount     int64                      `json:"maximumFileCount"`
	DenialReasons        []string                   `json:"denialReasons"`
}

// PromptMaterializationSnapshot combines exact run/session/template/context provenance, source and
// structured variables, capability sections and optional platform/user/artifact inputs for prompt
// assembly. Content or capability strings do not establish server authority; maps/slices are not
// deep-copied by the shape.
// PromptMaterializationSnapshot объединяет точное происхождение запуска, сессии, шаблона и контекста,
// исходные и структурированные переменные, разделы возможностей и необязательные platform, user и
// artifact inputs для сборки промпта. Текст и строки возможностей не доказывают серверные полномочия;
// форма не копирует maps и slices глубоко.
type PromptMaterializationSnapshot struct {
	AssistantCore               *PromptAssistantCore                  `json:"assistantCore,omitempty"`
	PlatformInstructions        *PromptPlatformInstructions           `json:"platformInstructions,omitempty"`
	ExtraTemplates              []PromptUserTemplate                  `json:"extraTemplates,omitempty"`
	Artifacts                   []runtimecontract.RunnerInputArtifact `json:"artifacts,omitempty"`
	UnavailableVariables        map[string]string                     `json:"unavailableVariables,omitempty"`
	StagePurposeTemplate        string                                `json:"stagePurposeTemplate,omitempty"`
	StageExpectedResultTemplate string                                `json:"stageExpectedResultTemplate,omitempty"`
	ServiceTemplateRevision     string                                `json:"serviceTemplateRevision,omitempty"`
	Locale                      string                                `json:"locale,omitempty"`
	SemanticValues              map[string]string                     `json:"semanticValues,omitempty"`
	ContextPin                  PromptContextPin                      `json:"contextPin,omitempty"`
	TargetKind                  string                                `json:"targetKind"`
	TargetRef                   string                                `json:"targetRef"`
	ProjectRef                  string                                `json:"projectRef"`
	RunRef                      string                                `json:"runRef"`
	SessionRef                  string                                `json:"sessionRef"`
	TemplateRef                 string                                `json:"templateRef"`
	TemplateDigest              string                                `json:"templateDigest"`
	TemplateContent             string                                `json:"templateContent"`
	Variables                   map[string]string                     `json:"variables"`
	StructuredVariables         map[string]any                        `json:"structuredVariables"`
	UserCapabilities            []string                              `json:"userCapabilities"`
	AgentCapabilities           []string                              `json:"agentCapabilities"`
	WorkflowCapabilities        []string                              `json:"workflowCapabilities"`
	ConnectionCapabilities      []string                              `json:"connectionCapabilities"`
	HumanGateCapabilities       []string                              `json:"humanGateCapabilities"`
	WorkflowStage               string                                `json:"workflowStage"`
	Automation                  string                                `json:"automation"`
	SessionContinuation         string                                `json:"sessionContinuation"`
}

// PromptAssistantCore pins platform-owned assistant core scope/reference/revision/digest/content
// separately from owner overlays; the server resolves identity/scope, not user prompt text.
// PromptAssistantCore закрепляет область, ссылку, ревизию, хеш и текст платформенного ядра помощника
// отдельно от owner overlays; личность и область разрешает сервер, не пользовательский текст промпта.
type PromptAssistantCore struct {
	Scope, Ref, Revision, Digest, Content string
}

// PromptPlatformInstructions carries the server-owned platform instruction snapshot and its
// provenance, not a grant or a user-editable owner overlay; the record itself does not verify the
// digest.
// PromptPlatformInstructions несёт серверный снимок платформенных инструкций и происхождение, не grant
// и не редактируемый пользователем owner overlay; сама запись хеш не проверяет.
type PromptPlatformInstructions struct {
	Ref, Revision, Digest, Content string
}

// PromptContextPin records exact agent/workflow/runtime/environment/attachment dependencies and
// previous runtime revision for a materialized prompt; a caller-supplied matching-looking digest is
// not authority or owner provenance.
// PromptContextPin фиксирует точные зависимости агента, процесса, runtime, окружения, вложений и
// предыдущей runtime revision материализованного промпта; похожий хеш от вызывающего не доказывает
// полномочия или происхождение владельца.
type PromptContextPin struct {
	DependencyDigest                                                       string `json:"DependencyDigest,omitempty"`
	Digest, AgentRef, WorkflowRef, WorkflowRevisionRef, WorkflowStageKey   string
	AgentVersion, WorkflowVersion                                          int64
	RuntimeConfigurationRef, RuntimeConfigurationDigest                    string
	EnvironmentBindingRef, EnvironmentVersionRef, EnvironmentDigest        string
	EnvironmentBindingVersion                                              int64
	AttachmentSetRef, AttachmentManifestDigest, PreviousRuntimeRevisionRef string
}

// ManagedConfigurationRevision groups source availability, optional prompt scope, exact
// source/parent/digest/revision and validation/publication metadata. Validation diagnostics and dates
// do not cause a lifecycle transition by themselves.
// ManagedConfigurationRevision объединяет доступность исходника, необязательную область промпта,
// точные текст, родителя, хеш и ревизию и метаданные проверки и публикации. Диагностика и даты сами
// переход жизненного цикла не вызывают.
type ManagedConfigurationRevision struct {
	SourceAvailable                                               *bool
	PromptScope                                                   *PromptTemplateScope
	Ref, State, ContentFormat, Content, Digest, ParentRevisionRef string
	Revision                                                      int64
	ValidationDiagnostics                                         []string
	CreatedAt                                                     time.Time
	ValidatedAt, PublishedAt                                      *time.Time
}

// PromptTemplateScope binds a template kind to a target kind/reference and exact prompt context pin,
// not arbitrary ownership inferred from those references.
// PromptTemplateScope связывает вид шаблона с видом, ссылкой цели и точной привязкой контекста, не
// произвольным владением из этих ссылок.
type PromptTemplateScope struct {
	TargetKind, TargetRef, TemplateKind string
	ContextPin                          PromptContextPin
}

// PromptUserTemplate keeps original template kind/reference/digest/content with an optional rendered
// user-task result; original and rendered digests remain distinct.
// PromptUserTemplate сохраняет исходные вид, ссылку, хеш и текст шаблона с необязательным
// отрендеренным результатом пользовательской задачи; исходный и итоговый хеши остаются разными.
type PromptUserTemplate struct {
	Kind, Ref, Digest, Content string
	Rendered                   *PromptRenderedUserTask `json:",omitempty"`
}

// PromptRenderedUserTask pairs rendered user-task content with its own digest, rather than replacing
// template provenance or platform instruction ownership.
// PromptRenderedUserTask связывает итоговый текст пользовательской задачи с собственным хешем, не
// заменяя происхождение шаблона или владение платформенными инструкциями.
type PromptRenderedUserTask struct {
	Content, Digest string
}

// ManagedConfigurationSet is the versioned configuration metadata/current-revision view with UI/Git
// ownership/source provenance, optional copy/Git bindings and edit/action flags. These fields do not
// authorize detach, copy or update commands.
// ManagedConfigurationSet — версионное представление метаданных конфигурации и текущей ревизии с
// UI/Git владением, происхождением, необязательными copy/Git связями и признаками редактирования и
// действий. Поля не разрешают команды detach, copy или update.
type ManagedConfigurationSet struct {
	NextActions                                                    []string
	Archived                                                       bool
	CopyProvenance                                                 *ManagedConfigurationCopyProvenance
	SourceEditable                                                 *bool
	Ref, ProjectRef, Kind, Name, ManagedBy, Source, SourceRevision string
	Version                                                        int64
	CurrentRevision                                                *ManagedConfigurationRevision
	UpdatedAt                                                      time.Time
	GitSource                                                      *ManagedConfigurationGitSource
}

// ManagedConfigurationCopyProvenance records the origin reference/revision/digest and version of a
// copied configuration; it is lineage metadata, not authority to mutate the source.
// ManagedConfigurationCopyProvenance фиксирует исходные источник, ссылку, ревизию, хеш и версию копии
// конфигурации; это происхождение, не полномочие изменять источник.
type ManagedConfigurationCopyProvenance struct {
	Origin, SourceRef, SourceRevision, SourceDigest string
	SourceVersion                                   int64
}

// ManagedConfigurationConsumer pins one consumer kind/reference/version/revision or expected absence
// for rebind checks; ExpectedAbsent is an expectation to verify, not proof that no binding exists.
// ManagedConfigurationConsumer закрепляет вид, ссылку, версию и ревизию потребителя либо ожидаемое
// отсутствие для перепривязки; ExpectedAbsent — проверяемое ожидание, не доказательство отсутствия
// связи.
type ManagedConfigurationConsumer struct {
	Kind, Ref, RevisionRef string
	Version                int64
	ExpectedAbsent         bool
}

// ManagedConfigurationImpact pages the affected consumers of an exact configuration/target revision
// under an impact digest; a page is not itself permission to apply rebinding.
// ManagedConfigurationImpact возвращает страницу затронутых потребителей точных конфигурации и целевой
// ревизии под хешем влияния; страница сама не разрешает перепривязку.
type ManagedConfigurationImpact struct {
	ConfigurationRef, TargetRevisionRef, Digest string
	Consumers                                   []ManagedConfigurationConsumer
	Total                                       int64
	NextPageToken                               string
}

// ManagedConfigurationBindingSnapshot joins a versioned consumer binding with the exact configuration
// and revision read alongside it; the nested values do not deep-copy mutable fields automatically.
// ManagedConfigurationBindingSnapshot соединяет версионную связь потребителя с точными прочитанными
// конфигурацией и ревизией; вложенные значения не копируют изменяемые поля глубоко автоматически.
type ManagedConfigurationBindingSnapshot struct {
	Ref, ConsumerKind, ConsumerRef string
	Version                        int64
	Configuration                  ManagedConfigurationSet
	Revision                       ManagedConfigurationRevision
}

// SystemSTTConfiguration carries the exact managed revision/digest, provider account/generation,
// model/language/parameters and audio/time limits together with readiness blockers; it contains no
// provider credential bytes and does not authorize transcription.
// SystemSTTConfiguration несёт точные управляемые ревизию и хеш, аккаунт и поколение провайдера,
// модель, язык, параметры и пределы аудио и времени с препятствиями готовности; байтов credentials не
// содержит и транскрипцию не разрешает.
type SystemSTTConfiguration struct {
	Parameters                                                                       value.STTParameters
	Enabled                                                                          bool
	MaximumAudioBytes, MaximumAudioDurationMilliseconds, ProviderTimeoutMilliseconds uint64
	ConfigurationRef, RevisionRef, Digest, ProviderAccountRef                        string
	Model, Language, PermissionKey                                                   string
	Revision                                                                         int64
	ProviderCredentialGeneration                                                     uint64
	Ready                                                                            bool
	ReadinessBlockers                                                                []string
}

// SpeechTranscriptionAvailability is the eligibility/reason view for offering transcription, distinct
// from starting a capture, obtaining consent or performing an STT request.
// SpeechTranscriptionAvailability — представление допустимости и причины показа транскрипции,
// отдельное от начала записи, получения согласия или STT-запроса.
type SpeechTranscriptionAvailability struct {
	Eligible bool
	Reason   string
}

// ProviderAuthorization exposes one short-lived authorization flow's method/state, verification
// URI/user code, expiry and safe failure. The internal materializer attempt is JSON-excluded; the
// value is not a provider token and should not be indiscriminately logged.
// ProviderAuthorization представляет короткоживущий вход: метод, состояние, verification URI, user
// code, срок и безопасную ошибку. Внутренняя попытка materializer исключена из JSON; значение не
// является токеном провайдера и не предназначено для бесконтрольного журналирования.
type ProviderAuthorization struct {
	Ref, Method, State, VerificationURI, UserCode, SafeFailureCode string
	ExpiresAt                                                      *time.Time
	MaterializerAttemptRef                                         string `json:"-"`
}

// ProviderCredentialDescriptor pins the exact provider secret object name/UID/resource version and
// content digest without exposing the stored authorization material.
// ProviderCredentialDescriptor закрепляет точные имя, UID, resource version и хеш объекта секрета
// провайдера без выдачи хранимого материала авторизации.
type ProviderCredentialDescriptor struct {
	SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
}

// ProviderAccount composes versioned account identity/state, masked external identity, readiness,
// concurrency limit, authorization, usage/finance eligibility and lifecycle views; no flag alone
// establishes an execution claim or permission to expose credentials.
// ProviderAccount соединяет версионные личность и состояние аккаунта, скрытую внешнюю личность,
// готовность, лимит параллельности, авторизацию, использование, финансовую допустимость и жизненный
// цикл; ни один флаг сам не доказывает захват выполнения или право раскрытия credentials.
type ProviderAccount struct {
	FinancialSummary                                                         *ProviderFinancialSummary
	FinancialEligible                                                        bool
	MaximumConcurrentExecutions                                              int32
	Usage                                                                    *ProviderAccountUsage
	Deletion                                                                 *ProviderAccountDeletion
	Verification                                                             *ProviderAccountVerification
	Ref, DefinitionKey, Name, ExternalAccountMasked, State, SafeStatusReason string
	Version                                                                  int64
	Enabled, Ready                                                           bool
	Authorization                                                            *ProviderAuthorization
	NextActions                                                              []string
	CreatedAt, UpdatedAt                                                     time.Time
}

// AgentAvatar identifies an avatar source and optional artifact revision/content path; it carries
// retrieval metadata rather than host-path or artifact-read authorization.
// AgentAvatar определяет источник аватара и необязательные ревизию артефакта и путь содержимого; это
// метаданные получения, не разрешение доступа к пути хоста или артефакту.
type AgentAvatar struct {
	Source, ArtifactRef, ContentPath string
	ArtifactRevision                 int64
}

// Agent groups project/role/runtime identity, display/state/version, instruction versions/binding,
// capability references and avatar metadata. System/assistant-backed flags are descriptive server
// state, not privileges granted by constructing a value.
// Agent объединяет проект, роль, runtime, отображение, состояние, версию, версии и связь инструкций,
// ссылки возможностей и аватар. System и assistant-backed — описательное серверное состояние, не
// привилегии от создания значения.
type Agent struct {
	AssistantBacked                                                   bool `json:"-"`
	CurrentRunRef                                                     string
	InstructionBinding                                                *AgentInstructionsBinding
	Ref, ProjectRef, RoleDefinitionRef, RoleDefinitionName, SystemKey string
	Name, Purpose, RoleDescription, AvatarURL                         string
	State, RuntimeKey, RuntimeName, Provider, Model, RuntimeRevision  string
	Enabled, System                                                   bool
	Version                                                           int64
	Capabilities, IntegrationGrantRefs, KnowledgeArtifactRefs         []string
	DraftInstructions, PublishedInstructions                          *InstructionVersion
	PublishedInstructionVersions                                      []InstructionVersion
	CreatedAt, UpdatedAt                                              time.Time
	NextActions                                                       []string
	Avatar                                                            AgentAvatar
}

// AgentInstructionsBinding identifies an exact versioned instruction revision binding and whether the
// domain reports it effective; the boolean does not publish or activate instructions.
// AgentInstructionsBinding определяет точную версионную связь ревизии инструкций и сообщаемую доменом
// действительность; флаг инструкции не публикует и не активирует.
type AgentInstructionsBinding struct {
	Ref, RevisionRef string
	Version          int64
	Effective        bool
}

// WorkflowInputField describes one workflow input's key/label/type, help/default, required flag and
// options for forms; it is schema metadata, not validation of a submitted value.
// WorkflowInputField описывает ключ, название, тип, справку, default, обязательность и варианты ввода
// процесса для форм; это метаданные схемы, не проверка переданного значения.
type WorkflowInputField struct {
	Key, Label, Type, Help, DefaultValue string
	Required                             bool
	Options                              []string
}

// WorkflowStep describes one agent stage's instruction/result, ordering/dependencies, parallel/timeout
// and gate/capability requirements; the shape does not schedule work or grant these capabilities.
// WorkflowStep описывает этап агента: инструкции, результат, порядок, зависимости, параллельность,
// срок и требования gate и возможностей; форма работу не планирует и возможности не выдаёт.
type WorkflowStep struct {
	Key, Name, AgentRef, Instructions, ExpectedResult string
	Position, ParallelGroup, TimeoutSeconds           int32
	Parallel, HumanGateAfter                          bool
	DependsOn, GateDecisions, RequiredCapabilityKeys  []string
	// ResultRequirements pin mandatory evidence for an explicitly server-driven published step.
	// ResultRequirements закрепляет обязательные доказательства явно серверного опубликованного этапа.
	ResultRequirements []WorkflowResultRequirement `json:",omitempty"`
	// ApprovalTimeoutSeconds optionally bounds owner waiting after this step completes; absence
	// preserves indefinite gate waiting subject to the independent workflow deadline.
	// ApprovalTimeoutSeconds ограничивает ожидание решения после завершения этапа; отсутствие
	// сохраняет прежнее ожидание с независимым ограничением общего срока процесса.
	ApprovalTimeoutSeconds *int32 `json:",omitempty"`
}

// WorkflowVersion is a numbered executable-definition snapshot containing coordinator/task text,
// ordered steps/agents, inputs/result schema and concurrency/time/gate settings; constructing it does
// not publish or start a workflow.
// WorkflowVersion — нумерованный снимок исполняемого определения с координатором, текстом, этапами,
// агентами, входом, схемой результата и настройками параллельности, времени и gate; создание значения
// процесс не публикует и не запускает.
type WorkflowVersion struct {
	// ExecutionMode is an explicit immutable policy; absence retains historical manager orchestration.
	// ExecutionMode — явная неизменяемая политика; отсутствие сохраняет прежнюю оркестрацию менеджером.
	ExecutionMode                                                             string `json:",omitempty"`
	Ref, Name, Purpose, CoordinatorAgentRef, Instructions, CompletionCriteria string
	VersionNumber                                                             int32
	Inputs                                                                    []WorkflowInputField
	Steps                                                                     []WorkflowStep
	AgentRefs                                                                 []string
	Concurrency                                                               int32
	TimeoutSeconds                                                            int64
	GateDecisions                                                             []string
	ResultSchema                                                              map[string]any
}

// Workflow combines versioned project metadata and optional draft/published definitions with
// card/readiness views; its state and next actions describe owner results rather than executing
// transitions.
// Workflow объединяет версионные метаданные проекта и необязательные черновое и опубликованное
// определения с карточкой и готовностью; состояние и действия описывают результат владельца, не
// выполняя переходы.
type Workflow struct {
	CardSummary                                                *WorkflowCardSummary
	LaunchReadiness                                            *WorkflowLaunchReadiness
	Ref, ProjectRef, Name, Purpose, CoordinatorAgentRef, State string
	Version                                                    int64
	Draft, Published                                           *WorkflowVersion
	CreatedAt, UpdatedAt                                       time.Time
	NextActions                                                []string
}

// WorkflowCardSummary records stage/agent/parallel/gate counts, active work and last activity for a
// compact card, not a replacement for the full workflow definition.
// WorkflowCardSummary фиксирует количество этапов, агентов, групп параллельности, gate, активной
// работы и последнюю активность для краткой карточки, не заменяя полное определение процесса.
type WorkflowCardSummary struct {
	StageCount, UniqueAgentCount, ParallelGroupCount int32
	HasHumanGate                                     bool
	ActiveRunCount, PendingGateCount                 int32
	LastActivityAt                                   *time.Time
}

// WorkflowLaunchReadiness ties a submission allowance/reason and operational state to exact workflow
// version/revision/context digest; it is a view to recheck at submission, not a reusable execution
// grant.
// WorkflowLaunchReadiness связывает допустимость отправки, причину и рабочее состояние с точными
// версией, ревизией и хешем контекста процесса; это повторно проверяемое при отправке view, не
// многоразовый grant выполнения.
type WorkflowLaunchReadiness struct {
	AllowedToSubmit                                      bool
	Reason, RevisionRef, ContextDigest, OperationalState string
	WorkflowVersion                                      int64
}

// RunTarget identifies the selected execution target's type/reference/name and version, leaving
// eligibility and current-version resolution to the owner.
// RunTarget определяет вид, ссылку, имя и версию выбранной цели выполнения, оставляя допустимость и
// разрешение текущей версии владельцу.
type RunTarget struct {
	Type, Ref, Name string
	Version         int64
}

// TokenUsage records total/input/output counts, independently bounded cached/cache-write and reasoning
// subsets, plus reported model context capacity. Values remain unvalidated until Valid is called and
// contain no pricing or quota decision.
// TokenUsage фиксирует общие, входные и выходные токены, отдельно ограниченные cached, cache-write и
// reasoning подмножества и заявленную ёмкость контекста модели. Значения не проверены до вызова Valid
// и не содержат решения о цене или квоте.
type TokenUsage struct {
	Completeness          runtimecontract.UsageCompleteness `json:"completeness,omitempty"`
	TotalTokens           int64                             `json:"total_tokens"`
	InputTokens           int64                             `json:"input_tokens"`
	CachedInputTokens     int64                             `json:"cached_input_tokens"`
	CacheWriteInputTokens int64                             `json:"cache_write_input_tokens"`
	OutputTokens          int64                             `json:"output_tokens"`
	ReasoningOutputTokens int64                             `json:"reasoning_output_tokens"`
	ModelContextWindow    int64                             `json:"model_context_window"`
}

// Valid requires every count/capacity to be nonnegative, total to equal input plus output, each
// cached/cache-write count to fit input and reasoning to fit output. It does not bound cached plus
// cache-write together, compare usage to the context window or decide provider credits.
// Valid требует неотрицательности всех счётчиков и ёмкости, равенства total сумме input и output,
// отдельного ограничения cached и cache-write входом и reasoning выходом. Сумму cached и cache-write
// вместе не ограничивает, использование с context window не сравнивает и доступность кредитов
// провайдера не решает.
func (usage TokenUsage) Valid() bool {
	return usage.Completeness.Valid() && usage.TotalTokens >= 0 && usage.InputTokens >= 0 && usage.CachedInputTokens >= 0 &&
		usage.CacheWriteInputTokens >= 0 && usage.OutputTokens >= 0 && usage.ReasoningOutputTokens >= 0 &&
		usage.ModelContextWindow >= 0 && usage.TotalTokens == usage.InputTokens+usage.OutputTokens &&
		usage.CachedInputTokens <= usage.InputTokens && usage.CacheWriteInputTokens <= usage.InputTokens &&
		usage.ReasoningOutputTokens <= usage.OutputTokens
}

// AssistantRunPin records server-resolved assistant scope,
// organization/conversation/assistant/project/profile lineage for a run; references alone do not
// authorize switching that lineage.
// AssistantRunPin фиксирует разрешённые сервером область помощника и происхождение организации,
// диалога, помощника, проекта и профиля запуска; ссылки сами смену происхождения не разрешают.
type AssistantRunPin struct {
	Scope, OrganizationRef, ConversationRef, AssistantRef, ProjectRef, ProfileRef string
}

// Run composes versioned session/root/parent/retry lineage, target/attempt/state, ordered graph/event
// markers, input/result/usage and optional assistant/readiness views. Content and mutable collections
// are not automatically redacted, frozen or authorized by this shape.
// Run объединяет версионное происхождение сессии, корневого и родительского запусков и повтора, цель,
// попытку, состояние, маркеры графа и событий, ввод, результат, использование и необязательные
// assistant/readiness views. Текст и изменяемые коллекции форма автоматически не скрывает, не
// замораживает и не авторизует.
type Run struct {
	Ref, ProjectRef, SessionRef, RootRunRef, ParentRunRef, RetryOfRunRef string
	InputAttachmentSetRef                                                string
	Title, Task, State, Source, ResultSummary, SafeErrorCode             string
	SafeErrorMessage, InitiatorName, TitleSource, ActivitySummary        string
	Target                                                               RunTarget
	Attempt                                                              int32
	GraphRevision, EventSequence, Version                                int64
	Input                                                                map[string]any
	Usage                                                                TokenUsage
	ArtifactRefs, GateRefs, NextActions                                  []string
	Incidents                                                            []Incident
	AssistantPin                                                         *AssistantRunPin
	SessionReadiness                                                     *RunSessionReadiness
	CreatedAt                                                            time.Time
	StartedAt, FinishedAt                                                *time.Time
}

// RunNode describes one graph execution node with parent/agent/turn/attempt,
// progress/callback/materialization/error summaries and child/artifact links; displayed state does not
// itself commit a terminal transition.
// RunNode описывает узел графа с родителем, агентом, ходом, попыткой, сводками прогресса, callback,
// материализации и ошибки и ссылками детей и артефактов; отображаемое состояние само terminal-переход
// не фиксирует.
type RunNode struct {
	// WorkflowResults exposes owner-accepted business history, not declarations from model output.
	// WorkflowResults передаёт проверенную владельцем бизнес-историю, не заявления модели.
	WorkflowResults                                                      []WorkflowAttemptResult `json:",omitempty"`
	Ref, RunRef, ParentNodeRef, Type, State, DisplayName, Role, AgentRef string
	TurnRef, InputSummary, ProgressSummary, CallbackSummary              string
	SafeErrorCode, SafeErrorMessage, MaterializationState                string
	Attempt                                                              int32
	IntegrationNames, ArtifactRefs, ChildRunRefs, NextActions            []string
	CreatedAt                                                            time.Time
	StartedAt, FinishedAt                                                *time.Time
}

// RunEdge is a typed, labeled source-to-target link within one run graph; creating this data record
// does not prove server-owned delegation or create a child grant.
// RunEdge — типизированная связь источника и цели графа запуска с меткой; создание записи не
// доказывает серверное делегирование и не создаёт дочерний grant.
type RunEdge struct {
	Ref, RunRef, SourceNodeRef, TargetNodeRef, Type, Label string
	ServerWorkflowRetry                                    *WorkflowRetryReadProof `json:",omitempty"`
}

// WorkflowRetryReadProof exposes only owner-verified historical SERVER step lineage; it neither
// authorizes another attempt nor carries private execution, grant or input provenance.
// WorkflowRetryReadProof передаёт лишь проверенную владельцем историю SERVER-этапа; не разрешает
// новую попытку и не содержит приватного происхождения execution, grant или входных данных.
type WorkflowRetryReadProof struct {
	WorkflowVersionRef, WorkflowVersionDigest, OriginNodeRef, StepKey string
	PreviousBusinessAttempt, BusinessAttempt                          int32
}

// RunDelta carries a run's changed state/result/error, version/graph/event sequence, usage, references
// and times for realtime projection; consumers still bind it to their exact run and monotonic version.
// RunDelta несёт изменённые состояние, результат, ошибку, версии запуска и графа, последовательность
// событий, использование, ссылки и сроки для realtime-проекции; потребитель по-прежнему связывает
// данные с точным запуском и монотонной версией.
type RunDelta struct {
	Ref, State, ResultSummary, SafeErrorCode, SafeErrorMessage string
	Version, GraphRevision, EventSequence                      int64
	Usage                                                      TokenUsage
	ArtifactRefs, GateRefs, NextActions                        []string
	StartedAt, FinishedAt                                      *time.Time
}

// RunEventDelta groups optional run/node/edge/gate/artifact/incident/execution/message changes and an
// optional integration invocation reference. The Go shape does not enforce exclusivity, eligibility or
// atomic persistence of these fields.
// RunEventDelta объединяет необязательные изменения запуска, узла, ребра, gate, артефакта, инцидента,
// исполнения и сообщения и ссылку вызова интеграции. Go-форма не обеспечивает взаимоисключение,
// допустимость или атомарное сохранение полей.
type RunEventDelta struct {
	IntegrationInvocationRef string `json:",omitempty"`
	Run                      *RunDelta
	Node                     *RunNode
	Edge                     *RunEdge
	Gate                     *OwnerGate
	Artifact                 *Artifact
	Incident                 *Incident
	Execution                *RunEventExecution
	Message                  *RunMessage
}

// RunEventExecution pins event execution context to exact run/node/session/turn plus turn number and
// attempt, avoiding an unscoped association by display text alone.
// RunEventExecution закрепляет контекст события с точными запуском, узлом, сессией, ходом, номером
// хода и попыткой, не сводя привязку лишь к отображаемому тексту.
type RunEventExecution struct {
	RunRef, NodeRef, SessionRef, TurnRef string
	TurnNumber                           int64
	Attempt                              int32
}

// RunMessage carries one revisioned message's identity/phase/text and source origin; message text is
// content, not system authority or proof of a user action.
// RunMessage несёт личность, фазу, текст и происхождение сообщения с ревизией; текст является
// содержимым, не системными полномочиями или доказательством действия пользователя.
type RunMessage struct {
	Ref, Phase, Text string
	Revision         int64
	Source           MessageSource
}

// MessageSource records the message's origin label without authenticating a sender or deriving
// permissions from that string.
// MessageSource фиксирует метку происхождения сообщения без аутентификации отправителя или вывода
// полномочий из строки.
type MessageSource struct {
	Origin string
}

// RunEventActor provides displayable actor kind/reference/name for an event, distinct from the
// verified transport principal used to authorize the underlying operation.
// RunEventActor задаёт отображаемые вид, ссылку и имя исполнителя события отдельно от проверенного
// principal транспорта, разрешающего исходную операцию.
type RunEventActor struct {
	Kind, Ref, Name string
}

// RunToolCall exposes a revisioned tool event with safe parameter/result projections,
// capability/grant/audit references, state and duration; field names do not themselves redact
// arbitrary supplied values or authorize execution.
// RunToolCall представляет версионное событие инструмента с безопасными проекциями параметров и
// результата, ссылками capability, grant и audit, состоянием и длительностью; имена полей сами
// произвольные значения не скрывают и выполнение не разрешают.
type RunToolCall struct {
	Ref            string         `json:"ref"`
	Tool           string         `json:"tool"`
	SafeParameters map[string]any `json:"safeParameters"`
	CapabilityRef  string         `json:"capabilityRef,omitempty"`
	GrantRef       string         `json:"grantRef,omitempty"`
	State          string         `json:"state"`
	DurationMS     int64          `json:"durationMs"`
	SafeResult     string         `json:"safeResult"`
	AuditRef       string         `json:"auditRef"`
	Revision       int64          `json:"revision,omitempty"`
}

// RunEvent combines exact aggregate/node/resource links, event sequence/graph revision, timestamps,
// actor and optional tool/delta views; storing the shape alone does not publish a domain event or
// validate routing.
// RunEvent соединяет точные ссылки агрегата, узла и ресурсов, последовательность события, ревизию
// графа, время, исполнителя и необязательные tool/delta views; сохранение формы само доменное событие
// не публикует и маршрутизацию не проверяет.
type RunEvent struct {
	Ref, RunRef, Type, NodeRef, EdgeRef, GateRef, ArtifactRef, IncidentRef string
	Summary, Progress, RunState, NodeState, MessageKind                    string
	Sequence, GraphRevision                                                int64
	OccurredAt                                                             time.Time
	Delta                                                                  RunEventDelta
	Actor                                                                  RunEventActor
	ToolCall                                                               *RunToolCall
}

// RunGraph is the graph read model at an exact run/revision/sequence with ordered nodes and edges; the
// auxiliary Runs collection is excluded from JSON and is not a new graph owner.
// RunGraph — модель чтения графа точных запуска, ревизии и последовательности с упорядоченными узлами
// и рёбрами; вспомогательные Runs исключены из JSON и новым владельцем графа не являются.
type RunGraph struct {
	Runs               []Run `json:"-"`
	RunRef             string
	Revision, Sequence int64
	Nodes              []RunNode
	Edges              []RunEdge
}

// OwnerGate describes a versioned human decision with scope/run/node context, allowed
// decisions/consequences, attachment provenance and optional integration intent. State/Decision fields
// are not a server-owned approval receipt by themselves.
// OwnerGate описывает версионное решение человека с областью, контекстом запуска и узла, разрешёнными
// решениями и последствиями, происхождением вложений и необязательным намерением интеграции. Поля
// State и Decision сами не являются серверным подтверждением.
type OwnerGate struct {
	ScopeKind, OrganizationRef                                      string
	Ref, ProjectRef, RunRef, NodeRef, Title, Prompt, ContextSummary string
	ResolutionAttachmentSetRef                                      string
	State, Decision, DecisionComment, RequestedByRef                string
	RequestedByName, ResolvedByName                                 string
	AllowedDecisions, NextActions                                   []string
	Version                                                         int64
	CreatedAt                                                       time.Time
	ResolvedAt                                                      *time.Time
	ExpiresAt                                                       *time.Time `json:",omitempty"`
	SourceAttachmentSetRef                                          string
	DecisionConsequences                                            []OwnerGateDecisionConsequence
	IntegrationIntent                                               *IntegrationIntent
}

// OwnerGateDecisionConsequence explains whether one allowed decision would execute an external effect
// or terminate a run, without executing either effect or transition.
// OwnerGateDecisionConsequence объясняет, приведёт ли разрешённое решение к внешнему эффекту или
// завершению запуска, не выполняя эффект или переход.
type OwnerGateDecisionConsequence struct {
	Decision, SafeSummary                  string
	ExecutesExternalEffect, TerminalForRun bool
}

// IntegrationIntent describes the exact connection/capability/operation/effect and resource-scope
// digest with a preview for a gate; preview data is not credential material or an approved external
// invocation.
// IntegrationIntent описывает точные подключение, возможность, операцию, эффект и хеш области ресурса
// с preview для gate; preview не является credential или подтверждённым внешним вызовом.
type IntegrationIntent struct {
	ConnectionRef, ConnectionName, DefinitionKey, CapabilityKey, Operation, EffectKey string
	ResourceKind, ResourceScopeDigest                                                 string
	ResourceScope                                                                     map[string]string
	EffectPreview                                                                     map[string]any
}

// Artifact is versioned file metadata and current-revision/scan/preview/lifecycle state with
// run/session/node links and trash deadlines, not file bytes, a storage locator or a permission to
// read quarantined content.
// Artifact — версионные метаданные файла, текущей ревизии, сканирования, preview и жизненного цикла со
// ссылками запуска, сессии, узла и сроками корзины, не байты, storage locator или разрешение чтения
// карантина.
type Artifact struct {
	Ref, ProjectRef, RunRef, SessionRef, NodeRef, FileName, MediaType, Digest string
	CurrentRevisionRef                                                        string
	ScanState, PreviewState, Source, LifecycleState                           string
	SizeBytes, Revision, Version                                              int64
	Bindings, NextActions                                                     []string
	CreatedAt                                                                 time.Time
	DeletedAt, PurgeAfter                                                     *time.Time
}

// ArtifactRevision records one exact file revision's digest/size/media/scan/source metadata and
// preview availability, deliberately without a storage locator or automatic admission decision.
// ArtifactRevision фиксирует хеш, размер, тип, сканирование, источник и доступность preview точной
// ревизии файла, намеренно без storage locator или автоматического решения о допуске.
type ArtifactRevision struct {
	Ref, ArtifactRef, FileName, MediaType, Digest, ScanState, Source string
	Revision, SizeBytes                                              int64
	PreviewAvailable                                                 bool
	CreatedAt                                                        time.Time
}

// ArtifactImpact summarizes a proposed artifact action under version/digest pins,
// binding/attachment/runtime counts, blockers and optionally truncated active-run references;
// Permitted is a view, not execution of the action.
// ArtifactImpact описывает предлагаемое действие с артефактом под привязками версии и хеша,
// количеством связей, вложений и runtime, препятствиями и возможным усечением списка активных
// запусков; Permitted — view, не выполнение действия.
type ArtifactImpact struct {
	ArtifactRef, Action, Digest                       string
	ArtifactVersion                                   int64
	BindingCount, AttachmentCount, ActiveRuntimeCount int64
	Blockers                                          []string
	Permitted                                         bool
	ActiveRuns                                        []ArtifactImpactRun
	ActiveRunsTruncated                               bool
}

// ArtifactImpactRun is the compact run/project identity, title and state used to explain active
// artifact consumers without embedding the full run input or graph.
// ArtifactImpactRun — краткие личность запуска и проекта, название и состояние для объяснения активных
// потребителей артефакта без полного ввода или графа запуска.
type ArtifactImpactRun struct {
	RunRef     string `json:"runRef"`
	Title      string `json:"title"`
	State      string `json:"state"`
	ProjectRef string `json:"projectRef"`
}

// AttachmentSetItem pins one positioned attachment to exact artifact revision/version/digest and
// display/media/source/size metadata; display name is not a filesystem destination.
// AttachmentSetItem закрепляет позиционное вложение с точными ревизией, версией и хешем артефакта и
// метаданными отображения, типа, источника и размера; имя не является путём записи файла.
type AttachmentSetItem struct {
	ArtifactRef, DisplayName, MediaType, Digest, Source string
	ArtifactRevision, ArtifactVersion, Position         int64
	SizeBytes                                           int64
}

// AttachmentSet groups a versioned family/project/purpose attachment manifest with ordered items,
// totals, finalization time and superseded state. A ManifestDigest field alone does not finalize or
// authorize using the set.
// AttachmentSet объединяет версионный manifest вложений семейства, проекта и назначения с
// упорядоченными элементами, количествами, временем фиксации и состоянием замещения. Поле
// ManifestDigest само набор не фиксирует и использование не разрешает.
type AttachmentSet struct {
	Ref, FamilyRef, ProjectRef, State, Purpose, Source, ManifestDigest string
	Revision, Version, ItemCount, TotalSizeBytes                       int64
	Items                                                              []AttachmentSetItem
	CreatedAt                                                          time.Time
	FinalizedAt                                                        *time.Time
	Superseded                                                         bool
}

// Schedule composes versioned automation timing/target/input with current revision, session
// continuation, notification and DST/misfire/overlap policies plus next/last execution views; it does
// not compute due occurrences or claim work.
// Schedule соединяет версионные время, цель и ввод автоматизации с текущей ревизией, продолжением
// сессии, уведомлениями, политиками DST, пропусков и перекрытия и данными следующего и прошлого
// выполнения; наступившие события не вычисляет и работу не захватывает.
type Schedule struct {
	Ref, ProjectRef, Name, Preset, CronExpression, TimeOfDay, DayOfWeek, Timezone string
	SessionPolicy, NotificationPolicy, State                                      string
	Target                                                                        RunTarget
	Input                                                                         map[string]any
	Enabled                                                                       bool
	Version                                                                       int64
	NextRunAt, LastRunAt                                                          *time.Time
	CreatedAt, UpdatedAt                                                          time.Time
	NextActions                                                                   []string
	CurrentRevision                                                               ScheduleRevision
	ContinueSessionRef                                                            string
	LastOutcome                                                                   string
	DSTGapPolicy, DSTFoldPolicy, MisfirePolicy, OverlapPolicy, TargetDigest       string
	TargetVersion                                                                 int64
	AutomationText                                                                string
	PromptInputs                                                                  map[string]any
}

// SchedulePromptPreviewPin identifies the exact schedule/revision/base, timezone/session, execution
// actor/mode and occurrence time used by a preview, preserving revision-availability and continuation
// flags without launching work.
// SchedulePromptPreviewPin определяет точные расписание, ревизию, основу, часовой пояс, сессию,
// исполнителя, режим и время события preview, сохраняя доступность ревизии и продолжение без запуска
// работы.
type SchedulePromptPreviewPin struct {
	ScheduleRef, RevisionRef, RevisionDigest, Timezone, SessionRef string
	Mode, ExecutionActorRef, BaseRevisionRef, BaseRevisionDigest   string
	ScheduleVersion                                                int64
	ScheduledFor                                                   time.Time
	Continuation, RevisionAvailable                                bool
}

// ScheduleRevision preserves the numbered/digested automation definition with target version/digest,
// timing/DST/misfire/overlap/session policies and prompt inputs; ordinary maps remain mutable despite
// snapshot semantics.
// ScheduleRevision сохраняет нумерованное определение автоматизации с хешем, версией и хешем цели,
// временем, политиками DST, пропусков, перекрытия и сессии и prompt inputs; обычные maps остаются
// изменяемыми несмотря на смысл снимка.
type ScheduleRevision struct {
	Ref, Digest, Name, Preset, CronExpression, Timezone, SessionPolicy, NotificationPolicy string
	DSTGapPolicy, DSTFoldPolicy, MisfirePolicy, OverlapPolicy, TargetDigest                string
	Revision                                                                               int64
	TargetVersion                                                                          int64
	Target                                                                                 RunTarget
	Input, PromptInputs                                                                    map[string]any
	AutomationText                                                                         string
	CreatedAt                                                                              time.Time
}

// ScheduleRunOccurrence associates a resulting run with the exact schedule revision reference/number
// that produced it, rather than the schedule's mutable current definition.
// ScheduleRunOccurrence связывает итоговый запуск с точными ссылкой и номером создавшей его ревизии
// расписания, а не изменяемым текущим определением.
type ScheduleRunOccurrence struct {
	ScheduleRef, ScheduleRevisionRef string
	ScheduleRevision                 int64
	Run                              Run
}

// IntegrationCapability describes one advertised operation's risk/approval options, resource kind and
// input schema/digest; catalog declaration does not assign an execution grant or replace per-effect
// confirmation.
// IntegrationCapability описывает риск, варианты подтверждения, вид ресурса и схему с хешем ввода
// предлагаемой операции; декларация каталога не выдаёт grant и не заменяет подтверждение конкретного
// эффекта.
type IntegrationCapability struct {
	Key                     string                          `json:"key"`
	Name                    string                          `json:"name"`
	Description             string                          `json:"description"`
	Operation               string                          `json:"operation"`
	Risk                    string                          `json:"risk"`
	ApprovalPolicy          string                          `json:"approvalPolicy"`
	AllowedApprovalPolicies []string                        `json:"allowedApprovalPolicies"`
	ResourceKind            string                          `json:"resourceKind"`
	InputFields             []IntegrationConfigurationField `json:"inputFields"`
	InputSchema             string                          `json:"inputSchema"`
	InputSchemaSHA256       string                          `json:"inputSchemaSha256"`
}

// IntegrationConfigurationField supplies form/schema metadata including optional numeric/string limits
// and allowed values; pointer limits preserve unspecified versus explicit zero, without validating
// input themselves.
// IntegrationConfigurationField задаёт метаданные формы и схемы с необязательными числовыми и
// строковыми пределами и вариантами значений; указатели отличают отсутствие предела от явного нуля,
// сами ввод не проверяя.
type IntegrationConfigurationField struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Help          string   `json:"help"`
	ValueType     string   `json:"valueType"`
	Placeholder   string   `json:"placeholder,omitempty"`
	Format        string   `json:"format,omitempty"`
	AllowedValues []string `json:"allowedValues,omitempty"`
	Minimum       *int64   `json:"minimum,omitempty"`
	Maximum       *int64   `json:"maximum,omitempty"`
	MaximumLength int32    `json:"maximumLength,omitempty"`
	Required      bool     `json:"required"`
}

// IntegrationDefinition groups catalog identity/version/origin/digest, adapter
// ownership/route/readiness and capability/configuration metadata with connection counts.
// CredentialSecretKey is a field name, not secret content or permission to load it.
// IntegrationDefinition объединяет личность, версию, источник и хеш каталога, владение, маршрут и
// готовность адаптера, возможности, поля конфигурации и счётчики подключений. CredentialSecretKey —
// имя поля, не содержимое секрета или право его загрузки.
type IntegrationDefinition struct {
	NextActions                                                        []string
	Version, ConnectionCount, HealthyConnectionCount                   int64
	Key, Name, Description, Category, SchemaVersion, DefinitionVersion string
	Origin, Digest, Adapter, CredentialSecretKey                       string
	AdapterOwner, ExecutionRoute, AdapterReadiness                     string
	Optional, Enabled                                                  bool
	Capabilities                                                       []IntegrationCapability
	ConfigurationFields                                                []IntegrationConfigurationField
}

// AssistantIntegrationDefinition is the reduced definition-selection view containing public
// catalog/configuration metadata and capability keys, excluding connection instances and credential
// values; CredentialSecretKey still denotes only a key name.
// AssistantIntegrationDefinition — сокращённое view выбора определения с публичными метаданными
// каталога и конфигурации и ключами возможностей, без экземпляров подключений и значений credentials;
// CredentialSecretKey остаётся лишь именем ключа.
type AssistantIntegrationDefinition struct {
	Key, Name, Description, Category, Adapter, CredentialSecretKey, Origin string
	ConfigurationFields                                                    []IntegrationConfigurationField
	CapabilityKeys                                                         []string
}

// IntegrationCredentialRevision pins logical revision/time to the exact secret reference/UID/resource
// version/content digest without carrying decrypted credential bytes.
// IntegrationCredentialRevision связывает логическую ревизию и время с точными ссылкой, UID, resource
// version и хешем секрета без расшифрованных байтов credential.
type IntegrationCredentialRevision struct {
	Ref, SecretRef, SecretUID, SecretResourceVersion, ContentSHA256 string
	Revision                                                        int64
	CreatedAt                                                       time.Time
}

// IntegrationGrant describes a versioned, enabled capability binding to a target under exact
// connection version, risk/approval policy and resource-scope digest/paths. The record is not itself
// verification that an invocation is currently authorized.
// IntegrationGrant описывает версионную включённую связь возможности с целью под точными версией
// подключения, риском, политикой подтверждения, хешем и путями области ресурса. Запись сама не
// подтверждает, что вызов сейчас разрешён.
type IntegrationGrant struct {
	ConnectionVersion                                                     int64
	Ref, CapabilityKey, TargetType, TargetRef, TargetName, ApprovalPolicy string
	Risk, ResourceKind, ResourceScopeDigest                               string
	ResourceScope                                                         map[string]string
	ApprovalScopePaths                                                    []string
	Enabled                                                               bool
	Version                                                               int64
}

// IntegrationConnection composes definition/configuration provenance, public settings, exact
// credential revision, capability/grant and test/lifecycle views. Masked state and public field names
// do not redact arbitrary nested values automatically.
// IntegrationConnection соединяет происхождение определения и конфигурации, публичные настройки,
// точную ревизию credentials, возможности, grants, проверки и жизненный цикл. Названия masked и public
// сами произвольные вложенные значения автоматически не скрывают.
type IntegrationConnection struct {
	TestRequiresApproval                                                    bool
	Ref, DefinitionKey, DefinitionName, Name, State, MaskedCredentialsState string
	CredentialSecretKey                                                     string
	DefinitionVersion, DefinitionDigest                                     string
	DefinitionConfigurationBinding                                          *IntegrationDefinitionConfigurationBinding
	LastTestSummary                                                         string
	Enabled                                                                 bool
	Version                                                                 int64
	PublicConfiguration                                                     map[string]any
	CredentialRevision                                                      *IntegrationCredentialRevision
	Capabilities                                                            []IntegrationCapability
	Grants                                                                  []IntegrationGrant
	LastTestedAt                                                            *time.Time
	CreatedAt, UpdatedAt                                                    time.Time
	NextActions                                                             []string
	LifecycleState                                                          string
}

// IntegrationDefinitionConfigurationBinding is the authoritative addressed-read projection of binding
// state/version and configuration/revision; nil means no supplied projection, not proven absence or
// permission to assert ExpectedAbsent.
// IntegrationDefinitionConfigurationBinding — авторитетная проекция адресного чтения состояния, версии
// и конфигурации с ревизией; nil означает отсутствие переданной проекции, не доказанное отсутствие
// связи и не право утверждать ExpectedAbsent.
type IntegrationDefinitionConfigurationBinding struct {
	State, ConfigurationRef, RevisionRef string
	BindingVersion                       int64
}

// AssistantContextDescriptor records the current UI route and entity reference/name/optional version
// with allowed-operation hints; route text and hints are not substitutes for owner authorization.
// AssistantContextDescriptor фиксирует текущий UI-маршрут, ссылку, имя и необязательную версию
// сущности с подсказками разрешённых операций; текст маршрута и подсказки не заменяют авторизацию
// владельца.
type AssistantContextDescriptor struct {
	Route                             string
	EntityKind, EntityRef, EntityName string
	EntityVersion                     *int64
	AllowedOperations                 []string
}

// AssistantPlanTarget preserves target kind/name and optional exact reference/version for a proposed
// plan operation, keeping absent values distinct from explicitly supplied pins.
// AssistantPlanTarget сохраняет вид, имя и необязательные точные ссылку и версию цели предлагаемой
// операции плана, отличая отсутствие от явно переданных привязок.
type AssistantPlanTarget struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref,omitempty"`
	Name    string `json:"name"`
	Version *int64 `json:"version,omitempty"`
}

// AssistantPlanOperation carries a proposed typed action, target, expected version,
// before/after/parameter previews and selection/validation state. Internal Input/flat target fields
// are JSON-excluded adapter inputs; neither preview nor Permitted authorizes execution by itself.
// AssistantPlanOperation несёт предлагаемое типизированное действие, цель, ожидаемую версию,
// before/after и параметры preview, выбор и проверку. Внутренние Input и плоские поля цели исключены
// из JSON и служат вводом адаптеров; ни preview, ни Permitted сами выполнение не разрешают.
type AssistantPlanOperation struct {
	Key                string              `json:"ref"`
	Type               string              `json:"type"`
	Action             string              `json:"action"`
	Title              string              `json:"title"`
	Summary            string              `json:"summary"`
	Target             AssistantPlanTarget `json:"target"`
	Parameters         map[string]any      `json:"parameters"`
	Before             map[string]any      `json:"before"`
	After              map[string]any      `json:"after"`
	ExpectedVersion    *int64              `json:"expectedVersion,omitempty"`
	Selected           bool                `json:"selected"`
	Permitted          bool                `json:"permitted"`
	UnavailableReason  string              `json:"unavailableReason,omitempty"`
	ValidationProblems []string            `json:"validationProblems"`
	// Input and flat target fields are internal specialized-adapter inputs,
	// excluded from the JSON plan projection; they are not public authority.
	// Input и плоские поля цели — внутренний ввод специализированных адаптеров,
	// исключённый из JSON-проекции плана; публичными полномочиями они не являются.
	Input                 map[string]any `json:"-"`
	TargetKind, TargetRef string         `json:"-"`
}

// AssistantPlan ties version/revision/content digest and proposed operations to a
// conversation/project, with the exact validated revision, timestamps and optional application
// receipt; validation state is not execution or human approval.
// AssistantPlan связывает версию, ревизию, хеш текста и предлагаемые операции с диалогом и проектом,
// точной проверенной ревизией, сроками и необязательным receipt применения; проверка не равна
// исполнению или подтверждению человеком.
type AssistantPlan struct {
	Ref, ConversationRef, ProjectRef, Summary, State, ContentDigest string
	Version, Revision                                               int64
	ValidatedRevision                                               *int64
	Operations                                                      []AssistantPlanOperation
	ValidationProblems                                              []string
	CreatedAt                                                       time.Time
	ValidatedAt, AppliedAt                                          *time.Time
	Receipt                                                         *AssistantPlanReceipt
}

// AssistantPlanOperationReceipt records one applied operation's exact resource/outcome/audit reference
// and optional artifact revision, rather than inferring success from the original proposal.
// AssistantPlanOperationReceipt фиксирует точные ресурс, исход, audit reference и необязательную
// ревизию артефакта применённой операции, не выводя успех из исходного предложения.
type AssistantPlanOperationReceipt struct {
	OperationRef, ResourceRef, Outcome, AuditRef string
	ArtifactRevision                             *ArtifactRevision
}

// AssistantPlanConflict identifies a conflicting operation/target/field with expected and actual
// values; it is diagnostic data, not an instruction to overwrite the current owner value.
// AssistantPlanConflict определяет конфликтующую операцию, цель, поле и ожидаемое и фактическое
// значения; это диагностика, не указание перезаписать текущее значение владельца.
type AssistantPlanConflict struct {
	OperationRef, TargetRef, Field string
	Expected, Actual               any
}

// AssistantPlanReceipt records the exact applied plan revision, overall outcome, per-operation
// receipts/conflicts and audit/created-resource references with time; it does not itself perform or
// retry partial effects.
// AssistantPlanReceipt фиксирует точную применённую ревизию плана, общий исход, результаты и конфликты
// операций, ссылки audit и созданных ресурсов и время; сам частичные эффекты не выполняет и не
// повторяет.
type AssistantPlanReceipt struct {
	Ref, PlanRef, Outcome          string
	PlanRevision                   int64
	Operations                     []AssistantPlanOperationReceipt
	Conflicts                      []AssistantPlanConflict
	AuditRefs, CreatedResourceRefs []string
	CreatedAt                      time.Time
}

// AssistantTurn is one ordered conversation message/turn with actor display identity, text/state,
// attachment/run/version links, origin and optional completion time; actor text does not authenticate
// the sender.
// AssistantTurn — упорядоченное сообщение или ход диалога с отображаемой личностью исполнителя,
// текстом, состоянием, вложениями, запуском, версией, происхождением и необязательным временем
// завершения; текст исполнителя отправителя не аутентифицирует.
type AssistantTurn struct {
	Ref, Actor, ActorName, Content, State, AttachmentSetRef, RunRef string
	Sequence, RunVersion                                            int64
	CreatedAt                                                       time.Time
	CompletedAt                                                     *time.Time
	Source                                                          MessageSource
}

// AssistantConversation joins a versioned/titled session and exact assistant scope/profile/project
// lineage with context, turns/plans and latest plan; the nested history does not grant cross-session
// or cross-project access.
// AssistantConversation соединяет версионную именованную сессию и точное происхождение области,
// профиля и проекта помощника с контекстом, ходами, планами и последним планом; вложенная история
// межсессионного или межпроектного доступа не даёт.
type AssistantConversation struct {
	Ref, Title, ProjectRef, SessionRef, State         string
	AssistantScope, AssistantRef, AssistantProfileRef string
	TitleSource                                       string
	Version, TitleRevision                            int64
	Context                                           AssistantContextDescriptor
	Turns                                             []AssistantTurn
	Plans                                             []AssistantPlan
	LatestPlan                                        *AssistantPlan
	CreatedAt, UpdatedAt                              time.Time
}

// ProjectAssistantProfile pins a distinct versioned assistant to one project; executable configuration
// belongs to AgentRef rather than a copied system profile, and the record itself performs no
// activation.
// ProjectAssistantProfile закрепляет отдельного версионного помощника с одним проектом; исполняемая
// конфигурация принадлежит AgentRef, не копии системного профиля, а запись сама активацию не
// выполняет.
type ProjectAssistantProfile struct {
	Ref, ProjectRef, AgentRef, Name, State string
	Version                                int64
	CreatedAt, UpdatedAt                   time.Time
}

// SystemAssistant is the platform assistant's versioned identity/core/owner-instruction and
// desired/observed runtime/readiness view with limits, heartbeat and next actions; these fields do not
// create a singleton execution lease or authorize self-reconfiguration.
// SystemAssistant — версионное представление личности, ядра, owner-инструкций и желаемых и наблюдаемых
// runtime и готовности платформенного помощника с лимитами, heartbeat и действиями; поля не создают
// singleton lease выполнения и не разрешают самонастройку.
type SystemAssistant struct {
	Ref, StableKey, Name, Purpose, CorePromptRevision, OwnerInstructions  string
	RuntimeState, RuntimeRevision, DesiredRuntimeRevision, WarmSessionRef string
	Ready, System, Deletable                                              bool
	Version                                                               int64
	ResourceLimits                                                        map[string]any
	LastHeartbeatAt                                                       *time.Time
	UpdatedAt                                                             time.Time
	NextActions                                                           []string
}

// AuditEvent records actor/executor/source, action/resource/outcome/correlation and time for an
// owner-produced audit view; accepting a caller-populated record would not prove an audited effect
// occurred.
// AuditEvent фиксирует исполнителя, executor, источник, действие, ресурс, исход, correlation и время в
// audit view владельца; принятие записи от вызывающего не доказывает совершившийся аудируемый эффект.
type AuditEvent struct {
	Ref, ProjectRef, ActorRef, ActorName, Executor, Source                            string
	Action, ResourceKind, ResourceRef, ResourceName, Outcome, Summary, CorrelationRef string
	OccurredAt                                                                        time.Time
}

// Incident provides scoped category/severity/state and safe summary/next-step metadata with
// core-impact and creation time, rather than raw exception details or an automatic remediation
// command.
// Incident задаёт область, категорию, серьёзность, состояние и безопасные сводку и следующий шаг с
// влиянием на ядро и временем создания, не сырые исключения или автоматическую команду исправления.
type Incident struct {
	Ref, ProjectRef, RunRef, Category, Severity, State string
	SafeSummary, SafeNextStep                          string
	CoreAffected                                       bool
	CreatedAt                                          time.Time
}
