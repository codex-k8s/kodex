package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// managedDraftRequest exposes only the draft fields shared by generated requests. It supplies data for
// conversion, not mutation authority or ownership.
// managedDraftRequest предоставляет только общие поля черновика сгенерированных запросов. Это данные
// преобразования, а не полномочия изменения или владения.
type managedDraftRequest interface {
	// GetConfigurationRef returns the configuration selector whose ownership is resolved by the command
	// owner.
	// GetConfigurationRef возвращает выбор конфигурации, владение которой определяет владелец команды.
	GetConfigurationRef() string
	// GetProjectRef returns the requested project scope without authenticating membership in that project.
	// GetProjectRef возвращает запрошенную область проекта без удостоверения членства в нём.
	GetProjectRef() string
	// GetName returns editable presentation metadata, not the configuration's resource identity.
	// GetName возвращает редактируемое имя представления, а не ресурсную личность конфигурации.
	GetName() string
	// GetContentFormat returns the declared format that the owner validates against the configuration
	// kind.
	// GetContentFormat возвращает объявленный формат, проверяемый владельцем относительно вида
	// конфигурации.
	GetContentFormat() string
	// GetContent returns proposed configuration text; callers must not treat it as already validated or
	// safe to log.
	// GetContent возвращает предлагаемый текст конфигурации; его нельзя считать уже проверенным или
	// безопасным для логов.
	GetContent() string
}

// CopyRoleImageConfiguration dispatches the exact role-image copy command with configuration, recipe,
// project, and name selectors through managedMutation. It returns the typed configuration and revision
// without turning a copy into build or promotion authority.
// CopyRoleImageConfiguration направляет точную команду копирования образа с выбором конфигурации,
// рецепта, проекта и имени через managedMutation. Возвращаются типизированные конфигурация и ревизия;
// копирование не выдаёт права сборки или публикации.
func (server *Server) CopyRoleImageConfiguration(ctx context.Context, request *controlplanev1.CopyRoleImageConfigurationRequest) (*controlplanev1.CopyRoleImageConfigurationResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CopyRoleImageConfiguration_FullMethodName, command.CopyRoleImageConfiguration, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RecipeRef: request.GetRecipeRef(), ProjectRef: request.GetProjectRef(), Name: request.GetName()})
	return &controlplanev1.CopyRoleImageConfigurationResponse{Configuration: configuration, Revision: revision}, err
}

// CopyIntegrationDefinitionConfiguration maps an optional shipped definition's key, version, and
// digest into the specialized copy command. The owner resolves source eligibility; the typed response
// retains both configuration and revision.
// CopyIntegrationDefinitionConfiguration переносит ключ, версию и дайджест необязательного
// поставляемого определения в специализированную команду копирования. Допустимость исходника
// определяет владелец; типизированный ответ сохраняет конфигурацию и ревизию.
func (server *Server) CopyIntegrationDefinitionConfiguration(ctx context.Context, request *controlplanev1.CopyIntegrationDefinitionConfigurationRequest) (*controlplanev1.CopyIntegrationDefinitionConfigurationResponse, error) {
	input := command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), Name: request.GetName()}
	if shipped := request.GetShipped(); shipped != nil {
		input.DefinitionKey, input.DefinitionVersion, input.DefinitionDigest = shipped.GetKey(), shipped.GetDefinitionVersion(), shipped.GetDigest()
	}
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CopyIntegrationDefinitionConfiguration_FullMethodName, command.CopyIntegrationDefinitionConfiguration, request.GetMutation(), input)
	return &controlplanev1.CopyIntegrationDefinitionConfigurationResponse{Configuration: configuration, Revision: revision}, err
}

// ArchiveRoleImageConfiguration routes the exact archive RPC and role-image command with the
// configuration selector, returning only its aggregate projection. Archive eligibility and lifecycle
// effects remain with the command owner.
// ArchiveRoleImageConfiguration направляет точные RPC архивации и команду образа с выбором
// конфигурации, возвращая только представление агрегата. Допустимость архивации и эффекты жизненного
// цикла остаются у владельца команды.
func (server *Server) ArchiveRoleImageConfiguration(ctx context.Context, request *controlplanev1.ArchiveRoleImageConfigurationRequest) (*controlplanev1.ArchiveRoleImageConfigurationResponse, error) {
	configuration, _, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ArchiveRoleImageConfiguration_FullMethodName, command.ArchiveRoleImageConfiguration, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef()})
	return &controlplanev1.ArchiveRoleImageConfigurationResponse{Configuration: configuration}, err
}

// ArchiveIntegrationDefinitionConfiguration keeps the integration-definition archive method and
// command distinct from role-image archival. The shared mutation boundary handles principal and
// errors; this typed response intentionally omits a revision.
// ArchiveIntegrationDefinitionConfiguration сохраняет отдельные метод и команду архивации определения
// интеграции, не смешивая их с образом. Общая граница изменения обрабатывает субъекта и ошибки;
// типизированный ответ намеренно не включает ревизию.
func (server *Server) ArchiveIntegrationDefinitionConfiguration(ctx context.Context, request *controlplanev1.ArchiveIntegrationDefinitionConfigurationRequest) (*controlplanev1.ArchiveIntegrationDefinitionConfigurationResponse, error) {
	configuration, _, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ArchiveIntegrationDefinitionConfiguration_FullMethodName, command.ArchiveIntegrationDefinitionConfiguration, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef()})
	return &controlplanev1.ArchiveIntegrationDefinitionConfigurationResponse{Configuration: configuration}, err
}

// managedRevisionRequest exposes the configuration and exact revision selectors shared by validation
// and publication requests. Filling the fields neither resolves ownership nor substitutes the latest
// revision.
// managedRevisionRequest предоставляет общие для проверки и публикации выбор конфигурации и точной
// ревизии. Заполнение полей не разрешает владение и не подменяет ревизию последней.
type managedRevisionRequest interface {
	// GetConfigurationRef returns the owning configuration selector for the requested revision.
	// GetConfigurationRef возвращает выбор конфигурации-владельца запрошенной ревизии.
	GetConfigurationRef() string
	// GetRevisionRef returns the exact revision selector to validate or publish, not an unpinned latest
	// alias.
	// GetRevisionRef возвращает точную выбранную ревизию для проверки или публикации, а не незакреплённый
	// псевдоним последней.
	GetRevisionRef() string
}

// managedRebindRequest exposes configuration, revision, impact digest, and explicitly selected
// consumers for rebinding. The owner validates the full versioned selection before changing bindings.
// managedRebindRequest предоставляет конфигурацию, ревизию, дайджест влияния и явно выбранных
// потребителей перепривязки. До изменения связей владелец проверяет весь версионированный выбор.
type managedRebindRequest interface {
	// GetConfigurationRef returns the configuration whose consumers are being selected for rebinding.
	// GetConfigurationRef возвращает конфигурацию, потребители которой выбираются для перепривязки.
	GetConfigurationRef() string
	// GetRevisionRef returns the target revision to which eligible selected consumers may be rebound.
	// GetRevisionRef возвращает целевую ревизию для перепривязки допустимых выбранных потребителей.
	GetRevisionRef() string
	// GetImpactDigest returns the expected impact snapshot digest; a digest alone is not approval of the
	// selected effects.
	// GetImpactDigest возвращает ожидаемый дайджест снимка влияния; дайджест сам не подтверждает выбранные
	// эффекты.
	GetImpactDigest() string
	// GetConsumers returns typed consumer selectors with revision, version, and expected-absence pins for
	// owner validation.
	// GetConsumers возвращает типизированный выбор потребителей с привязками ревизии, версии и ожидаемого
	// отсутствия для проверки владельцем.
	GetConsumers() []*controlplanev1.ManagedConfigurationConsumer
}

// managedDraftInput copies the common draft selectors, name, format, and content into the domain input
// without validation or redaction. Prompt scope is attached separately by the prompt-specific RPC.
// managedDraftInput копирует общие признаки выбора черновика, имя, формат и содержимое в доменный вход
// без проверки и сокрытия. Область промпта отдельно добавляет специализированный RPC.
func managedDraftInput(request managedDraftRequest) command.ManagedConfigurationInput {
	return command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), ProjectRef: request.GetProjectRef(),
		Name: request.GetName(), ContentFormat: request.GetContentFormat(), Content: request.GetContent()}
}

// managedRevisionInput maps only configuration and revision references into the domain input. It does
// not load the revision or add authority, plan, or consumer selection.
// managedRevisionInput переносит в доменный вход только ссылки конфигурации и ревизии. Функция не
// загружает ревизию и не добавляет полномочия, план или выбор потребителей.
func managedRevisionInput(request managedRevisionRequest) command.ManagedConfigurationInput {
	return command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef()}
}

// managedRebindInput maps the impact digest and ordered consumer selectors, preserving their version,
// revision, and expected-absence values. An empty source keeps a nil consumer slice; generated getters
// determine zero values for nil entries.
// managedRebindInput переносит дайджест влияния и упорядоченный выбор потребителей, сохраняя версии,
// ревизии и ожидаемое отсутствие. Пустой источник сохраняет nil-срез; нулевые значения nil-элементов
// определяют сгенерированные getters.
func managedRebindInput(request managedRebindRequest) command.ManagedConfigurationInput {
	result := command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef(), ImpactDigest: request.GetImpactDigest()}
	for _, item := range request.GetConsumers() {
		result.Consumers = append(result.Consumers, entity.ManagedConfigurationConsumer{Kind: item.GetKind(), Ref: item.GetRef(), RevisionRef: item.GetRevisionRef(), Version: item.GetVersion(), ExpectedAbsent: item.GetExpectedAbsent()})
	}
	return result
}

// managedMutation shares method-bound execution and result conversion for specialized managed
// commands. execute resolves the principal and mutation, including the existing correlation-key
// fallback; errors return before casting, and no arbitrary payload method is exposed here.
// managedMutation объединяет привязанное к методу исполнение и преобразование результата
// специализированных управляемых команд. execute определяет субъекта и изменение, включая прежний
// резервный ключ корреляции; ошибка возвращается до преобразования, произвольный метод данных здесь не
// публикуется.
func (server *Server) managedMutation(ctx context.Context, method string, kind command.Kind, mutation *controlplanev1.MutationContext, input command.ManagedConfigurationInput) (*controlplanev1.ManagedConfigurationSet, *controlplanev1.ManagedConfigurationRevision, error) {
	result, err := execute(ctx, server.service, method, kind, mutation, input)
	if err != nil {
		return nil, nil, err
	}
	return castManagedConfiguration(result.ManagedConfiguration), castManagedRevision(result.ManagedRevision), nil
}

// CreatePromptTemplateDraft adds the typed prompt target and context pins to the common draft input
// and dispatches the prompt-create method. The owner validates prompt scope; conversion does not grant
// access to the target.
// CreatePromptTemplateDraft добавляет типизированные привязки цели и контекста промпта к общему входу
// черновика и вызывает метод создания шаблона. Область проверяет владелец; преобразование не даёт
// доступа к цели.
func (server *Server) CreatePromptTemplateDraft(ctx context.Context, request *controlplanev1.CreatePromptTemplateDraftRequest) (*controlplanev1.CreatePromptTemplateDraftResponse, error) {
	input := managedDraftInput(request)
	input.PromptScope = castPromptScopeInput(request.GetPromptScope())
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CreatePromptTemplateDraft_FullMethodName, command.CreatePromptTemplateDraft, request.GetMutation(), input)
	return &controlplanev1.CreatePromptTemplateDraftResponse{Configuration: configuration, Revision: revision}, err
}

// ValidatePromptTemplateDraft requests validation of the exact prompt configuration revision through
// the specialized method and command. Validation is distinct from publishing or rebinding consumers.
// ValidatePromptTemplateDraft запрашивает проверку точной ревизии конфигурации промпта через
// специализированные метод и команду. Проверка отличается от публикации и перепривязки потребителей.
func (server *Server) ValidatePromptTemplateDraft(ctx context.Context, request *controlplanev1.ValidatePromptTemplateDraftRequest) (*controlplanev1.ValidatePromptTemplateDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ValidatePromptTemplateDraft_FullMethodName, command.ValidatePromptTemplateDraft, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.ValidatePromptTemplateDraftResponse{Configuration: configuration, Revision: revision}, err
}

// PublishPromptTemplateDraft copies selected impact items while preserving nil for an empty selection,
// executes the specialized publication, and verifies the returned applied PROMPT_TEMPLATE plan before
// exposing configuration, revision, and plan. A malformed owner result fails through transportError.
// PublishPromptTemplateDraft копирует выбранные элементы влияния с сохранением nil пустого выбора,
// исполняет специализированную публикацию и проверяет итоговый применённый план PROMPT_TEMPLATE до
// выдачи конфигурации, ревизии и плана. Некорректный результат владельца отклоняется через
// transportError.
func (server *Server) PublishPromptTemplateDraft(ctx context.Context, request *controlplanev1.PublishPromptTemplateDraftRequest) (*controlplanev1.PublishPromptTemplateDraftResponse, error) {
	payload := managedRevisionInput(request)
	payload.PlanRef = request.GetPlanRef()
	payload.SelectedItemRefs = append([]string(nil), request.GetSelectedItemRefs()...)
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishPromptTemplateDraft_FullMethodName, command.PublishPromptTemplateDraft, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	if err := validatePublishedImpactResult(result, "PROMPT_TEMPLATE", request.GetPlanRef(), request.GetConfigurationRef()); err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.PublishPromptTemplateDraftResponse{Configuration: castManagedConfiguration(result.ManagedConfiguration), Revision: castManagedRevision(result.ManagedRevision), Plan: castRevisionImpactPlan(result.RevisionImpactPlan)}, nil
}

// RebindPromptTemplateConsumers routes exact prompt-consumer rebinding with consumer and impact pins
// through the shared typed input and mutation boundary. It does not reuse another configuration kind's
// command or response.
// RebindPromptTemplateConsumers направляет точную перепривязку потребителей промпта с привязками
// потребителей и влияния через общие типизированный вход и границу изменения. Команда и ответ другого
// вида конфигурации не переиспользуются.
func (server *Server) RebindPromptTemplateConsumers(ctx context.Context, request *controlplanev1.RebindPromptTemplateConsumersRequest) (*controlplanev1.RebindPromptTemplateConsumersResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_RebindPromptTemplateConsumers_FullMethodName, command.RebindPromptTemplate, request.GetMutation(), managedRebindInput(request))
	return &controlplanev1.RebindPromptTemplateConsumersResponse{Configuration: configuration, Revision: revision}, err
}

// CreateRoleImageRevisionDraft creates a proposed managed image revision from the common draft input
// using its own method and command. Creating the draft does not build, admit, or promote an image.
// CreateRoleImageRevisionDraft создаёт предлагаемую управляемую ревизию образа из общего входа
// черновика собственными методом и командой. Создание черновика не выполняет сборку, допуск или
// публикацию образа.
func (server *Server) CreateRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.CreateRoleImageRevisionDraftRequest) (*controlplanev1.CreateRoleImageRevisionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CreateRoleImageRevisionDraft_FullMethodName, command.CreateRoleImageRevisionDraft, request.GetMutation(), managedDraftInput(request))
	return &controlplanev1.CreateRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// ValidateRoleImageRevisionDraft dispatches validation of the exact managed image revision and returns
// the owner-projected configuration and revision. Validation alone is not a supply-chain admission
// receipt.
// ValidateRoleImageRevisionDraft направляет проверку точной управляемой ревизии образа и возвращает
// представленные владельцем конфигурацию и ревизию. Сама проверка не является квитанцией допуска
// цепочки поставки.
func (server *Server) ValidateRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.ValidateRoleImageRevisionDraftRequest) (*controlplanev1.ValidateRoleImageRevisionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ValidateRoleImageRevisionDraft_FullMethodName, command.ValidateRoleImageRevision, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.ValidateRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// PublishRoleImageRevisionDraft routes managed image-revision publication through its exact
// specialized command. The result projection does not replace the separate build, admission, and
// promotion lifecycle.
// PublishRoleImageRevisionDraft направляет публикацию управляемой ревизии образа через точную
// специализированную команду. Представление результата не заменяет отдельный жизненный цикл сборки,
// допуска и публикации образа.
func (server *Server) PublishRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.PublishRoleImageRevisionDraftRequest) (*controlplanev1.PublishRoleImageRevisionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_PublishRoleImageRevisionDraft_FullMethodName, command.PublishRoleImageRevision, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.PublishRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// RebindRoleImageConsumers adds an impact plan and a copied nonnil selected-item slice to consumer
// pins, then executes the image-specific rebind and casts its role-image impact plan. It deliberately
// differs from prompt publication's additional result validation.
// RebindRoleImageConsumers добавляет план влияния и скопированный ненулевой срез выбранных элементов к
// привязкам потребителей, исполняет перепривязку образа и преобразует план влияния образа. Это
// намеренно отличается от дополнительной проверки результата публикации промпта.
func (server *Server) RebindRoleImageConsumers(ctx context.Context, request *controlplanev1.RebindRoleImageConsumersRequest) (*controlplanev1.RebindRoleImageConsumersResponse, error) {
	input := managedRebindInput(request)
	input.PlanRef, input.SelectedItemRefs = request.GetPlanRef(), append([]string{}, request.GetSelectedItemRefs()...)
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_RebindRoleImageConsumers_FullMethodName, command.RebindRoleImage, request.GetMutation(), input, func(result command.Result) *controlplanev1.RebindRoleImageConsumersResponse {
		return &controlplanev1.RebindRoleImageConsumersResponse{Configuration: castManagedConfiguration(result.ManagedConfiguration), Revision: castManagedRevision(result.ManagedRevision), Plan: castRoleImageImpactPlan(result.RoleImageImpactPlan)}
	})
}

// CreateIntegrationDefinitionDraft maps common editable draft fields into the specialized
// integration-definition create command. Definition validation and ownership resolution remain in the
// owner path.
// CreateIntegrationDefinitionDraft переносит общие редактируемые поля черновика в специализированную
// команду создания определения интеграции. Проверка определения и разрешение владения остаются в пути
// владельца.
func (server *Server) CreateIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.CreateIntegrationDefinitionDraftRequest) (*controlplanev1.CreateIntegrationDefinitionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CreateIntegrationDefinitionDraft_FullMethodName, command.CreateIntegrationDefinition, request.GetMutation(), managedDraftInput(request))
	return &controlplanev1.CreateIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// ValidateIntegrationDefinitionDraft validates the selected integration configuration revision through
// its exact method and command without publishing it or granting a capability.
// ValidateIntegrationDefinitionDraft проверяет выбранную ревизию конфигурации интеграции точными
// методом и командой без публикации или выдачи возможности.
func (server *Server) ValidateIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.ValidateIntegrationDefinitionDraftRequest) (*controlplanev1.ValidateIntegrationDefinitionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ValidateIntegrationDefinitionDraft_FullMethodName, command.ValidateIntegrationDefinition, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.ValidateIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// PublishIntegrationDefinitionDraft dispatches the exact integration-definition publication and
// returns its configuration and revision. Publication does not issue connection grants or execute
// provider operations.
// PublishIntegrationDefinitionDraft направляет точную публикацию определения интеграции и возвращает
// конфигурацию и ревизию. Публикация не выдаёт разрешения подключений и не исполняет операции
// провайдера.
func (server *Server) PublishIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.PublishIntegrationDefinitionDraftRequest) (*controlplanev1.PublishIntegrationDefinitionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_PublishIntegrationDefinitionDraft_FullMethodName, command.PublishIntegrationDefinition, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.PublishIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// RebindIntegrationDefinitionConsumers preserves the integration-specific RPC and command while
// sharing only consumer-input conversion and mutation plumbing. The owner rechecks impact and
// dependency pins before changing bindings.
// RebindIntegrationDefinitionConsumers сохраняет специализированные RPC и команду интеграции, разделяя
// только преобразование потребителей и выполнение изменения. До перепривязки владелец повторно
// проверяет влияние и зависимости.
func (server *Server) RebindIntegrationDefinitionConsumers(ctx context.Context, request *controlplanev1.RebindIntegrationDefinitionConsumersRequest) (*controlplanev1.RebindIntegrationDefinitionConsumersResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_RebindIntegrationDefinitionConsumers_FullMethodName, command.RebindIntegrationDefinition, request.GetMutation(), managedRebindInput(request))
	return &controlplanev1.RebindIntegrationDefinitionConsumersResponse{Configuration: configuration, Revision: revision}, err
}

// CreateSystemSTTConfigurationDraft creates the system speech-recognition draft through the exact STT
// command, using common editable fields. It neither requests microphone access nor starts
// transcription.
// CreateSystemSTTConfigurationDraft создаёт системный черновик распознавания речи точной командой STT
// с общими редактируемыми полями. Доступ к микрофону не запрашивается, распознавание не запускается.
func (server *Server) CreateSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.CreateSystemSTTConfigurationDraftRequest) (*controlplanev1.CreateSystemSTTConfigurationDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CreateSystemSTTConfigurationDraft_FullMethodName, command.CreateSystemSTTDraft, request.GetMutation(), managedDraftInput(request))
	return &controlplanev1.CreateSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}

// ValidateSystemSTTConfigurationDraft dispatches validation of one exact STT draft revision. A
// validation result is not evidence of a successful real audio or provider request.
// ValidateSystemSTTConfigurationDraft направляет проверку одной точной черновой ревизии STT. Результат
// проверки не доказывает успешный реальный запрос аудио или провайдера.
func (server *Server) ValidateSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.ValidateSystemSTTConfigurationDraftRequest) (*controlplanev1.ValidateSystemSTTConfigurationDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ValidateSystemSTTConfigurationDraft_FullMethodName, command.ValidateSystemSTTDraft, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.ValidateSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}

// PublishSystemSTTConfigurationDraft publishes the selected managed STT revision through its own
// method and command, returning typed configuration and revision projections. Runtime eligibility
// remains independently checked.
// PublishSystemSTTConfigurationDraft публикует выбранную управляемую ревизию STT собственными методом
// и командой, возвращая типизированные представления конфигурации и ревизии. Допустимость исполнения
// проверяется независимо.
func (server *Server) PublishSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.PublishSystemSTTConfigurationDraftRequest) (*controlplanev1.PublishSystemSTTConfigurationDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_PublishSystemSTTConfigurationDraft_FullMethodName, command.PublishSystemSTTDraft, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.PublishSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}

// RebindSystemSTTConsumers maps explicit consumer and impact pins into the exact STT rebind command.
// Shared conversion does not turn this method into a generic configuration mutation endpoint.
// RebindSystemSTTConsumers переносит явные привязки потребителей и влияния в точную команду
// перепривязки STT. Общее преобразование не делает метод универсальным endpoint изменения
// конфигураций.
func (server *Server) RebindSystemSTTConsumers(ctx context.Context, request *controlplanev1.RebindSystemSTTConsumersRequest) (*controlplanev1.RebindSystemSTTConsumersResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_RebindSystemSTTConsumers_FullMethodName, command.RebindSystemSTT, request.GetMutation(), managedRebindInput(request))
	return &controlplanev1.RebindSystemSTTConsumersResponse{Configuration: configuration, Revision: revision}, err
}

// DetachGitManagedConfiguration routes the explicit detach transition for one configuration through
// the method-bound owner path. It does not silently overwrite Git-owned content through an ordinary
// draft update.
// DetachGitManagedConfiguration направляет явный переход отключения одной конфигурации через
// привязанный к методу путь владельца. Обычное изменение черновика не перезаписывает незаметно
// Git-содержимое.
func (server *Server) DetachGitManagedConfiguration(ctx context.Context, request *controlplanev1.DetachGitManagedConfigurationRequest) (*controlplanev1.DetachGitManagedConfigurationResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DetachGitManagedConfiguration_FullMethodName, command.DetachGitManagedConfiguration, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef()})
	return &controlplanev1.DetachGitManagedConfigurationResponse{Configuration: configuration, Revision: revision}, err
}

// CopyGitManagedConfiguration requests a separate copy of a Git-managed configuration with a proposed
// name. Source ownership and copy provenance are resolved by the owner rather than changed by this
// request mapping.
// CopyGitManagedConfiguration запрашивает отдельную копию управляемой Git-конфигурации с предлагаемым
// именем. Владение исходником и происхождение копии определяет владелец, а не преобразование запроса.
func (server *Server) CopyGitManagedConfiguration(ctx context.Context, request *controlplanev1.CopyGitManagedConfigurationRequest) (*controlplanev1.CopyGitManagedConfigurationResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CopyGitManagedConfiguration_FullMethodName, command.CopyGitManagedConfiguration, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), Name: request.GetName()})
	return &controlplanev1.CopyGitManagedConfigurationResponse{Configuration: configuration, Revision: revision}, err
}

// ListManagedConfigurations authenticates the exact query method, accepts an unspecified kind as no
// kind filter, and rejects unknown nonzero enum values before calling the owner with project, search,
// and pagination. It preserves order while casting the eligible list and total.
// ListManagedConfigurations удостоверяет точный метод чтения, принимает неопределённый вид как
// отсутствие фильтра и отклоняет неизвестные ненулевые enum до обращения к владельцу с проектом,
// поиском и пагинацией. Порядок допустимого списка и общее количество сохраняются при преобразовании.
func (server *Server) ListManagedConfigurations(ctx context.Context, request *controlplanev1.ListManagedConfigurationsRequest) (*controlplanev1.ListManagedConfigurationsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListManagedConfigurations_FullMethodName)
	if err != nil {
		return nil, err
	}
	kind := ""
	if request.GetKind() != controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_UNSPECIFIED {
		name, known := controlplanev1.ManagedConfigurationKind_name[int32(request.GetKind())]
		if !known {
			return nil, status.Error(codes.InvalidArgument, "invalid configuration kind")
		}
		kind = strings.TrimPrefix(name, "MANAGED_CONFIGURATION_KIND_")
	}
	items, total, next, err := server.service.ListManagedConfigurations(ctx, p, query.Filter{
		ProjectRef: request.GetProjectRef(), Category: kind, Query: request.GetQuery(), Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListManagedConfigurationsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Configurations = append(response.Configurations, castManagedConfiguration(&item))
	}
	return response, nil
}

// ListManagedConfigurationHistory reads the selected configuration and revision page under the exact
// query principal, maps owner failures, and casts revisions in their original order. A history
// reference does not bypass owner eligibility.
// ListManagedConfigurationHistory читает выбранную конфигурацию и страницу ревизий с субъектом точного
// метода, преобразует ошибки владельца и сохраняет порядок ревизий. Ссылка истории не обходит
// допустимость владельца.
func (server *Server) ListManagedConfigurationHistory(ctx context.Context, request *controlplanev1.ListManagedConfigurationHistoryRequest) (*controlplanev1.ListManagedConfigurationHistoryResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListManagedConfigurationHistory_FullMethodName)
	if err != nil {
		return nil, err
	}
	configuration, revisions, total, next, err := server.service.ListManagedConfigurationHistory(ctx, p, request.GetConfigurationRef(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListManagedConfigurationHistoryResponse{Configuration: castManagedConfiguration(&configuration), Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for index := range revisions {
		response.Revisions = append(response.Revisions, castManagedRevision(&revisions[index]))
	}
	return response, nil
}

// GetManagedConfigurationImpact authenticates its exact method and reads the selected
// configuration/revision impact with explicit search and page fields. The returned impact projection
// is a read result, not approval of consumer changes.
// GetManagedConfigurationImpact удостоверяет точный метод и читает влияние выбранных конфигурации и
// ревизии с явными поиском и полями страницы. Проекция влияния является результатом чтения, а не
// подтверждением изменений потребителей.
func (server *Server) GetManagedConfigurationImpact(ctx context.Context, request *controlplanev1.GetManagedConfigurationImpactRequest) (*controlplanev1.GetManagedConfigurationImpactResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetManagedConfigurationImpact_FullMethodName)
	if err != nil {
		return nil, err
	}
	impact, err := server.service.GetManagedConfigurationImpact(ctx, p, request.GetConfigurationRef(), request.GetRevisionRef(), query.Filter{Query: request.GetQuery(), Page: query.Page{Size: request.GetPage().GetPageSize(), Token: request.GetPage().GetPageToken()}})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetManagedConfigurationImpactResponse{Impact: castManagedImpact(impact)}, nil
}

// GetRuntimeEnvironmentRoleImageConfiguration resolves the runtime-work principal and reads the
// ROLE_IMAGE binding for the selected RUNTIME_ENVIRONMENT. These fixed kinds remain server-side
// constants rather than caller-controlled dispatch.
// GetRuntimeEnvironmentRoleImageConfiguration определяет субъекта runtime-work и читает привязку
// ROLE_IMAGE выбранного RUNTIME_ENVIRONMENT. Фиксированные виды остаются серверными константами, а не
// управляемым вызывающим кодом выбором команды.
func (server *Server) GetRuntimeEnvironmentRoleImageConfiguration(ctx context.Context, request *controlplanev1.GetRuntimeEnvironmentRoleImageConfigurationRequest) (*controlplanev1.GetRuntimeEnvironmentRoleImageConfigurationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_GetRuntimeEnvironmentRoleImageConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	binding, err := server.service.GetEffectiveManagedConfiguration(ctx, p, "ROLE_IMAGE", "RUNTIME_ENVIRONMENT", request.GetEnvironmentRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRuntimeEnvironmentRoleImageConfigurationResponse{Binding: castManagedBinding(binding)}, nil
}

// GetIntegrationConnectionDefinitionConfiguration keeps its own runtime-work method and reads the
// INTEGRATION_DEFINITION binding for an INTEGRATION_CONNECTION. It shares result casting, not the
// environment-image method's authority or selectors.
// GetIntegrationConnectionDefinitionConfiguration сохраняет собственный метод runtime-work и читает
// привязку INTEGRATION_DEFINITION для INTEGRATION_CONNECTION. Общим является преобразование
// результата, а не полномочия или выбор метода образа окружения.
func (server *Server) GetIntegrationConnectionDefinitionConfiguration(ctx context.Context, request *controlplanev1.GetIntegrationConnectionDefinitionConfigurationRequest) (*controlplanev1.GetIntegrationConnectionDefinitionConfigurationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_GetIntegrationConnectionDefinitionConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	binding, err := server.service.GetEffectiveManagedConfiguration(ctx, p, "INTEGRATION_DEFINITION", "INTEGRATION_CONNECTION", request.GetConnectionRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetIntegrationConnectionDefinitionConfigurationResponse{Binding: castManagedBinding(binding)}, nil
}

// GetSystemSTTConfiguration authenticates the query and maps the owner-resolved STT revision,
// readiness, provider generation, limits, and parameters. It copies readiness, language, and keyword
// slices, carries no credential bytes, and does not perform transcription.
// GetSystemSTTConfiguration удостоверяет запрос и переносит определённые владельцем ревизию STT,
// готовность, поколение провайдера, лимиты и параметры. Срезы препятствий, языков и ключевых слов
// копируются, байты реквизитов не передаются, распознавание не выполняется.
func (server *Server) GetSystemSTTConfiguration(ctx context.Context, _ *controlplanev1.GetSystemSTTConfigurationRequest) (*controlplanev1.GetSystemSTTConfigurationResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetSystemSTTConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	configuration, err := server.service.GetSystemSTTConfiguration(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetSystemSTTConfigurationResponse{Configuration: &controlplanev1.SystemSTTConfiguration{
		ConfigurationRef: configuration.ConfigurationRef, RevisionRef: configuration.RevisionRef, Revision: configuration.Revision,
		Digest: configuration.Digest, ProviderAccountRef: configuration.ProviderAccountRef, Model: configuration.Model,
		Language: configuration.Language, PermissionKey: configuration.PermissionKey, Ready: configuration.Ready,
		ReadinessBlockers: append([]string(nil), configuration.ReadinessBlockers...), ProviderCredentialGeneration: configuration.ProviderCredentialGeneration,
		Enabled: configuration.Enabled, MaximumAudioBytes: configuration.MaximumAudioBytes,
		MaximumAudioDurationMilliseconds: configuration.MaximumAudioDurationMilliseconds, ProviderTimeoutMilliseconds: configuration.ProviderTimeoutMilliseconds,
		Parameters: &controlplanev1.SystemSTTParameters{Languages: append([]string(nil), configuration.Parameters.Languages...),
			Keywords: append([]string(nil), configuration.Parameters.Keywords...), Prompt: configuration.Parameters.Prompt,
			Temperature: configuration.Parameters.Temperature, ChunkingStrategy: configuration.Parameters.ChunkingStrategy, Stream: configuration.Parameters.Stream},
	}}, nil
}

// castManagedRevision preserves nil and projects revision state, content, provenance links,
// diagnostics, prompt scope, and timestamps into Proto. The diagnostics slice is copied; content
// remains configuration data, not automatically redacted log text.
// castManagedRevision сохраняет nil и переносит состояние ревизии, содержимое, связи происхождения,
// диагностику, область промпта и время в Proto. Срез диагностики копируется; содержимое остаётся
// конфигурацией, а не автоматически очищенным текстом лога.
func castManagedRevision(value *entity.ManagedConfigurationRevision) *controlplanev1.ManagedConfigurationRevision {
	if value == nil {
		return nil
	}
	return &controlplanev1.ManagedConfigurationRevision{Ref: value.Ref, Revision: value.Revision,
		SourceAvailable: value.SourceAvailable,
		PromptScope:     castPromptScope(value.PromptScope),
		State:           managedRevisionStateProto(value.State),
		ContentFormat:   value.ContentFormat, Content: value.Content, Digest: value.Digest,
		ValidationDiagnostics: append([]string(nil), value.ValidationDiagnostics...), ParentRevisionRef: value.ParentRevisionRef,
		CreatedAt: timestamp(value.CreatedAt), ValidatedAt: optionalTimestamp(value.ValidatedAt), PublishedAt: optionalTimestamp(value.PublishedAt)}
}

// castPromptScopeInput preserves nil and maps target, agent, workflow, stage, expected context digest,
// and template kind into the command scope. Prefix trimming is conversion only; unknown enum semantics
// are left to owner validation.
// castPromptScopeInput сохраняет nil и переносит цель, сотрудника, процесс, этап, ожидаемый дайджест
// контекста и вид шаблона в область команды. Удаление префикса является только преобразованием;
// семантику неизвестного enum проверяет владелец.
func castPromptScopeInput(input *controlplanev1.PromptTemplateScopeInput) *command.PromptTemplateScopeInput {
	if input == nil {
		return nil
	}
	return &command.PromptTemplateScopeInput{TargetKind: input.GetTargetKind(), TargetRef: input.GetTargetRef(), AgentRef: input.GetAgentRef(),
		WorkflowRevisionRef: input.GetWorkflowRevisionRef(), WorkflowStageKey: input.GetWorkflowStageKey(), ExpectedContextDigest: input.GetExpectedContextDigest(),
		TemplateKind: strings.TrimPrefix(input.GetTemplateKind().String(), "PROMPT_TEMPLATE_KIND_")}
}

// castPromptScope preserves nil and casts the public prompt target and context pin, looking up the
// template kind with the existing zero fallback for unknown strings. It does not revalidate the source
// snapshot.
// castPromptScope сохраняет nil и преобразует публичные цель промпта и привязку контекста с прежним
// нулевым значением неизвестного вида шаблона. Исходный снимок заново не проверяется.
func castPromptScope(input *entity.PromptTemplateScope) *controlplanev1.PromptTemplateScope {
	if input == nil {
		return nil
	}
	return &controlplanev1.PromptTemplateScope{TargetKind: input.TargetKind, TargetRef: input.TargetRef, ContextPin: castPromptContextPin(input.ContextPin),
		TemplateKind: controlplanev1.PromptTemplateKind(controlplanev1.PromptTemplateKind_value["PROMPT_TEMPLATE_KIND_"+input.TemplateKind])}
}

// managedRevisionStateProto maps DRAFT, VALID, INVALID, PUBLISHED, SUPERSEDED, and DISCARDED to their
// typed Proto states. Every other string maps to UNSPECIFIED rather than being treated as a valid
// published revision.
// managedRevisionStateProto переносит DRAFT, VALID, INVALID, PUBLISHED, SUPERSEDED и DISCARDED в
// типизированные состояния Proto. Остальные строки дают UNSPECIFIED, а не считаются допустимой
// опубликованной ревизией.
func managedRevisionStateProto(state string) controlplanev1.ManagedConfigurationState {
	switch state {
	case "DRAFT":
		return controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DRAFT
	case "VALID":
		return controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_VALID
	case "INVALID":
		return controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_INVALID
	case "PUBLISHED":
		return controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_PUBLISHED
	case "SUPERSEDED":
		return controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_SUPERSEDED
	case "DISCARDED":
		return controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DISCARDED
	default:
		return controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_UNSPECIFIED
	}
}

// castManagedConfiguration preserves nil and projects aggregate identity, ownership mode, source and
// copy provenance, current revision, Git source, editability, and actions. Actions use a copied nonnil
// empty slice; unknown enum strings keep the generated zero-value lookup behavior.
// castManagedConfiguration сохраняет nil и переносит личность агрегата, режим владения, исходник и
// происхождение копии, текущую ревизию, Git-источник, редактируемость и действия. Действия копируются
// с ненулевым пустым срезом; неизвестные enum сохраняют прежнее нулевое значение lookup.
func castManagedConfiguration(value *entity.ManagedConfigurationSet) *controlplanev1.ManagedConfigurationSet {
	if value == nil {
		return nil
	}
	return &controlplanev1.ManagedConfigurationSet{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef,
		Archived: value.Archived, CopyProvenance: castConfigurationCopyProvenance(value.CopyProvenance), NextActions: append([]string{}, value.NextActions...),
		SourceEditable: value.SourceEditable,
		Kind:           controlplanev1.ManagedConfigurationKind(controlplanev1.ManagedConfigurationKind_value["MANAGED_CONFIGURATION_KIND_"+value.Kind]),
		Name:           value.Name, ManagedBy: controlplanev1.ManagedConfigurationOwner(controlplanev1.ManagedConfigurationOwner_value["MANAGED_CONFIGURATION_OWNER_"+value.ManagedBy]),
		Source: value.Source, SourceRevision: value.SourceRevision, CurrentRevision: castManagedRevision(value.CurrentRevision), UpdatedAt: timestamp(value.UpdatedAt), GitSource: castConfigurationSource(value.GitSource)}
}

// castConfigurationCopyProvenance preserves nil and projects copy origin and exact source reference,
// revision, version, and digest. Enum lookup keeps its existing zero fallback and does not establish
// verified provenance by itself.
// castConfigurationCopyProvenance сохраняет nil и переносит происхождение копии и точные ссылку,
// ревизию, версию и дайджест исходника. Поиск enum сохраняет прежнее нулевое значение и сам не
// удостоверяет происхождение.
func castConfigurationCopyProvenance(value *entity.ManagedConfigurationCopyProvenance) *controlplanev1.ManagedConfigurationCopyProvenance {
	if value == nil {
		return nil
	}
	return &controlplanev1.ManagedConfigurationCopyProvenance{Origin: controlplanev1.ManagedConfigurationCopyOrigin(controlplanev1.ManagedConfigurationCopyOrigin_value["MANAGED_CONFIGURATION_COPY_ORIGIN_"+value.Origin]), SourceRef: value.SourceRef, SourceRevision: value.SourceRevision, SourceVersion: value.SourceVersion, SourceDigest: value.SourceDigest}
}

// castManagedConsumer projects only the consumer kind, reference, revision, and version into the
// response. This read projection does not carry the command input's ExpectedAbsent flag or validate
// consumer ownership.
// castManagedConsumer переносит в ответ только вид, ссылку, ревизию и версию потребителя. Проекция
// чтения не передаёт признак ExpectedAbsent входа команды и не проверяет владение потребителем.
func castManagedConsumer(value entity.ManagedConfigurationConsumer) *controlplanev1.ManagedConfigurationConsumer {
	return &controlplanev1.ManagedConfigurationConsumer{Kind: value.Kind, Ref: value.Ref, RevisionRef: value.RevisionRef, Version: value.Version}
}

// castManagedBinding combines the binding identity and version and fixed consumer selectors with
// converted configuration and revision snapshots. It does not resolve a new effective binding or
// refresh either snapshot.
// castManagedBinding объединяет личность и версию привязки и фиксированный выбор потребителя с
// преобразованными снимками конфигурации и ревизии. Новая фактическая привязка не разрешается, снимки
// не обновляются.
func castManagedBinding(value entity.ManagedConfigurationBindingSnapshot) *controlplanev1.ManagedConfigurationBindingSnapshot {
	return &controlplanev1.ManagedConfigurationBindingSnapshot{
		BindingRef: value.Ref, BindingVersion: value.Version, ConsumerKind: value.ConsumerKind, ConsumerRef: value.ConsumerRef,
		Configuration: castManagedConfiguration(&value.Configuration), Revision: castManagedRevision(&value.Revision),
	}
}

// castManagedImpact projects impact identity, target revision, digest, total, pagination, and ordered
// consumers. Empty consumer input preserves a nil slice; the result is not a mutable plan approval or
// a rebinding effect.
// castManagedImpact переносит личность влияния, целевую ревизию, дайджест, количество, пагинацию и
// порядок потребителей. Пустой вход потребителей сохраняет nil-срез; результат не является
// подтверждением изменяемого плана или эффектом перепривязки.
func castManagedImpact(value entity.ManagedConfigurationImpact) *controlplanev1.ManagedConfigurationImpact {
	result := &controlplanev1.ManagedConfigurationImpact{ConfigurationRef: value.ConfigurationRef, TargetRevisionRef: value.TargetRevisionRef, Digest: value.Digest, Total: value.Total, Page: &controlplanev1.PageInfo{NextPageToken: value.NextPageToken}}
	for _, item := range value.Consumers {
		result.Consumers = append(result.Consumers, castManagedConsumer(item))
	}
	return result
}
