package entity

import "github.com/codex-k8s/kodex/libs/go/runtimecontract"

// AssistantRuntimeProfilePin identifies a runtime profile by reference, aggregate version, and runtime
// revision. All three pins describe the selected snapshot; the structure does not verify admission or
// authorize use of another profile.
// AssistantRuntimeProfilePin определяет профиль исполнения ссылкой, версией агрегата и ревизией
// исполнения. Все три привязки описывают выбранный снимок; структура не проверяет допуск и не
// разрешает использование чужого профиля.
type AssistantRuntimeProfilePin struct {
	Ref             string `json:"ref"`
	Version         int64  `json:"version"`
	RuntimeRevision string `json:"runtimeRevision"`
}

// AssistantConfigurationCatalogRequest selects a typed assistant catalogue page or current
// configuration, with optional account, profile, and recipient context. The owner validates allowed
// field combinations and resolves them under the authenticated lease rather than trusting supplied
// scope references.
// AssistantConfigurationCatalogRequest выбирает типизированную страницу каталога помощника или текущую
// конфигурацию с необязательным контекстом аккаунта, профиля и получателя. Владелец проверяет
// сочетания полей и разрешает их в рамках удостоверенной аренды, а не доверяет переданным ссылкам
// области.
type AssistantConfigurationCatalogRequest struct {
	Kind, AssistantRef, Query, AccountRef, RuntimeProfileRef string
	Offset                                                   int32
	EntityKind, EntityRef                                    string
}

// AssistantConfigurationCatalogEntry describes a selectable resource with owner scope, catalogue and
// image pins, model reasoning options, and optional tool inventory. Reported admission and promotion
// states are selection evidence, not a new runtime grant or mutable latest-image alias.
// AssistantConfigurationCatalogEntry описывает выбираемый ресурс с областью владельца, привязками
// каталога и образа, вариантами рассуждения модели и необязательным перечнем инструментов. Состояния
// допуска и публикации служат свидетельствами выбора, а не новым разрешением исполнения или изменяемым
// псевдонимом последнего образа.
type AssistantConfigurationCatalogEntry struct {
	Ref, Name, Provider, Model, Reference, ManifestDigest       string
	ScopeKind, OrganizationRef, ProjectRef, AssistantProfileRef string
	CatalogRevision, CatalogDigest, DefaultReasoningEffort      string
	RuntimeEnvironmentRef                                       string
	EnvironmentKey                                              string
	ReasoningEfforts                                            []string
	Version, RecipeGeneration                                   int64
	AdmissionVerdict, PromotionState                            string
	ToolInventorySHA256                                         string
	ToolInventory                                               *runtimecontract.ImageToolInventory
}

// AssistantConfigurationCatalogResponse returns a typed catalogue page and optional configuration or
// grant projections for the resolved assistant scope. Optional fields are not enforced as an exclusive
// union by this structure; the producer must return the projection appropriate to Kind.
// AssistantConfigurationCatalogResponse возвращает типизированную страницу каталога и необязательные
// представления конфигурации или разрешений для разрешённой области помощника. Структура не
// обеспечивает исключение необязательных полей; производитель возвращает представление,
// соответствующее Kind.
type AssistantConfigurationCatalogResponse struct {
	Kind, AssistantRef, ScopeKind, OrganizationRef, ProjectRef, AssistantProfileRef string
	Entries                                                                         []AssistantConfigurationCatalogEntry
	NextOffset                                                                      int32
	CurrentConfiguration                                                            *AssistantCurrentConfiguration
	ProjectIntegrationGrants                                                        []ProjectAssistantIntegrationGrantCatalogEntry
	RecipientIntegrationGrants                                                      *AssistantRecipientIntegrationGrantCatalog
	WorkflowConfiguration                                                           *AssistantWorkflowConfiguration
	AgentConfiguration                                                              *AssistantAgentConfiguration
	AgentRuntimeConfiguration                                                       *AssistantAgentConfiguration
}

// AssistantAgentConfiguration carries an agent's configuration bytes with project, version, and
// content digest. The digest binds the bytes to a snapshot; it does not by itself establish ownership
// or make configuration text safe to execute.
// AssistantAgentConfiguration передаёт байты конфигурации сотрудника с проектом, версией и дайджестом
// содержимого. Дайджест связывает байты со снимком, но сам не устанавливает владение и не делает текст
// конфигурации безопасным для исполнения.
type AssistantAgentConfiguration struct {
	AgentRef, ProjectRef, ConfigurationSHA256 string
	Version                                   int64
	ConfigurationJSON                         []byte
}

// AssistantWorkflowConfiguration carries workflow configuration bytes pinned by project, version, and
// content digest. Reading this snapshot does not publish the workflow or authorize a launch using
// arbitrary supplied content.
// AssistantWorkflowConfiguration передаёт байты конфигурации процесса с привязками проекта, версии и
// дайджеста содержимого. Чтение снимка не публикует процесс и не разрешает запуск произвольного
// переданного содержимого.
type AssistantWorkflowConfiguration struct {
	WorkflowRef, ProjectRef, ConfigurationSHA256 string
	Version                                      int64
	ConfigurationJSON                            []byte
}

// AssistantRecipientIntegrationGrantCatalog describes integration candidates for a resolved recipient
// and context entity with their versions. Candidate entries support a later exact grant decision;
// listing them does not issue a grant.
// AssistantRecipientIntegrationGrantCatalog описывает кандидатов интеграций для разрешённых получателя
// и контекстной сущности с их версиями. Элементы служат для последующего точного решения о разрешении;
// перечисление их не выдаёт.
type AssistantRecipientIntegrationGrantCatalog struct {
	RecipientKind, RecipientRef, RecipientName string
	RecipientVersion, ProjectVersion           int64
	Entries                                    []AssistantRecipientIntegrationGrantCatalogEntry
	ContextEntityKind, ContextEntityRef        string
	ContextEntityVersion                       int64
}

// AssistantRecipientIntegrationGrantCatalogEntry pairs an integration candidate projection with the
// version and dependency pins required to validate its selection. The candidate and pins must remain
// associated; neither is standalone approval.
// AssistantRecipientIntegrationGrantCatalogEntry связывает представление кандидата интеграции с
// версиями и зависимостями, необходимыми для проверки выбора. Кандидат и привязки сохраняются вместе;
// ни один из них не является самостоятельным подтверждением.
type AssistantRecipientIntegrationGrantCatalogEntry struct {
	Grant ProjectAssistantIntegrationGrantCatalogEntry
	Pins  IntegrationCandidatePins
}

// AssistantCurrentConfiguration is the current configuration read model with pinned environment,
// secret bindings, overlays, instructions, and image tool inventory. The producer excludes
// materialized secret values and transport metadata; this structure is not itself a redaction
// mechanism or a permission to mutate the snapshot.
// AssistantCurrentConfiguration является моделью чтения текущей конфигурации с привязками окружения,
// секретов, слоёв, инструкций и инструментов образа. Производитель исключает материализованные
// значения секретов и метаданные транспорта; сама структура не скрывает данные и не разрешает
// изменение снимка.
type AssistantCurrentConfiguration struct {
	AgentVersion                                                             int64
	Configuration                                                            AgentRuntimeConfiguration
	PublishedOverlay                                                         ConfigOverlayVersion
	EnvironmentBinding                                                       AgentRuntimeEnvironmentBinding
	EnvironmentRef                                                           string
	EnvironmentVersion                                                       int64
	Environment                                                              RuntimeEnvironmentVersion
	SecretBindings                                                           []RuntimeSecretBinding
	InstructionTemplateRef, InstructionTemplateDigest, PublishedInstructions string
	SystemCoreRevision, SystemCoreInstructions, OwnerInstructions            string
	OwnerInstructionsRevision                                                int64
	TemplateVariables                                                        []TemplateVariable
	ImageToolInventorySHA256                                                 string
	ImageToolInventory                                                       *runtimecontract.ImageToolInventory
}
