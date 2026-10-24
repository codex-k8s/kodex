package httptransport

import (
	"net/http"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// GetAgentRuntimeConfiguration checks opaque reference syntax, reads the owner configuration and uses exact-agent/version/binding validation before public output; syntax alone does not grant access.
// GetAgentRuntimeConfiguration проверяет формат opaque-ссылки, читает конфигурацию владельца и перед выдачей сверяет сотрудника, версию и bindings; сам формат доступ не выдаёт.
func (server *Server) GetAgentRuntimeConfiguration(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef) {
	if !opaqueHTTPReference.MatchString(agentRef) {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetAgentRuntimeConfiguration(request.Context(), &controlplanev1.GetAgentRuntimeConfigurationRequest{AgentRef: agentRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

// ListAgentRuntimeConfigurationVersions forwards agent and page selectors to the owner and projects historical configuration metadata without activating any version.
// ListAgentRuntimeConfigurationVersions передаёт сотрудника и страницу владельцу и проецирует историческую metadata конфигураций без активации версии.
func (server *Server) ListAgentRuntimeConfigurationVersions(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.ListAgentRuntimeConfigurationVersionsParams) {
	response, err := server.control.Query.ListAgentRuntimeConfigurationVersions(request.Context(), &controlplanev1.ListAgentRuntimeConfigurationVersionsRequest{
		AgentRef: agentRef, Page: page(parameters.PageSize, parameters.PageToken),
	})
	writeReadRPCResult(writer, response, err, "", "configurations")
}

// PublishAgentRuntimeConfiguration decodes the body and mutation pins, validates distinct weighted provider candidates with exact catalog pins, then invokes the dedicated owner command and validates the returned agent view. Candidate display data is not an execution grant.
// PublishAgentRuntimeConfiguration разбирает тело и mutation pins, проверяет различные взвешенные аккаунты с точными pins каталога, вызывает отдельную команду владельца и проверяет представление сотрудника. Данные кандидатов не являются grant выполнения.
func (server *Server) PublishAgentRuntimeConfiguration(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.PublishAgentRuntimeConfigurationParams) {
	body, ok := decodeJSON[generated.AgentRuntimeConfigurationInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	candidates, valid := providerAccountCandidates(body.ProviderAccounts)
	if !valid {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Command.PublishAgentRuntimeConfiguration(request.Context(), &controlplanev1.PublishAgentRuntimeConfigurationRequest{
		Mutation: mutation, AgentRef: agentRef, RuntimeProfileRef: body.RuntimeProfileRef, Model: body.Model,
		ProviderPolicyMode: string(body.ProviderPolicyMode), ProviderAccounts: candidates,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

// CreateConfigOverlayDraft decodes content and mutation context, delegates draft creation to the owner and returns a validated exact-agent view with HTTP201; it does not publish the overlay.
// CreateConfigOverlayDraft разбирает content и mutation context, поручает создание черновика владельцу и возвращает проверенное представление сотрудника с HTTP201; overlay не публикуется.
func (server *Server) CreateConfigOverlayDraft(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.CreateConfigOverlayDraftParams) {
	body, ok := decodeJSON[generated.ConfigOverlayDraftInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.CreateConfigOverlayDraft(request.Context(), &controlplanev1.CreateConfigOverlayDraftRequest{Mutation: mutation, AgentRef: agentRef, Content: body.Content})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusCreated, response.GetRuntimeConfiguration(), agentRef)
}

// ValidateConfigOverlayDraft forwards mutation pins to the dedicated owner validation command and checks the returned agent configuration; local parsing is not a substitute for owner validation.
// ValidateConfigOverlayDraft передаёт mutation pins отдельной команде проверки владельца и сверяет возвращённую конфигурацию; локальный разбор проверку владельца не заменяет.
func (server *Server) ValidateConfigOverlayDraft(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.ValidateConfigOverlayDraftParams) {
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ValidateConfigOverlayDraft(request.Context(), &controlplanev1.ValidateConfigOverlayDraftRequest{Mutation: mutation, AgentRef: agentRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

// PublishConfigOverlayDraft invokes the dedicated owner publish command with mutation/version context, then projects the exact-agent view; this handler does not itself materialize runtime files.
// PublishConfigOverlayDraft вызывает отдельную команду публикации владельца с mutation/version context, затем проецирует точного сотрудника; runtime-файлы сам handler не материализует.
func (server *Server) PublishConfigOverlayDraft(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.PublishConfigOverlayDraftParams) {
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PublishConfigOverlayDraft(request.Context(), &controlplanev1.PublishConfigOverlayDraftRequest{Mutation: mutation, AgentRef: agentRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

// RollbackConfigOverlay decodes a selected published overlay and mutation context for the specialized owner rollback, preserving owner-side history/version policy and validated response identity.
// RollbackConfigOverlay разбирает выбранный опубликованный overlay и mutation context для специального rollback владельца, сохраняя его политику истории и версий и проверяемую identity ответа.
func (server *Server) RollbackConfigOverlay(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.RollbackConfigOverlayParams) {
	body, ok := decodeJSON[generated.ConfigOverlayRollbackInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RollbackConfigOverlay(request.Context(), &controlplanev1.RollbackConfigOverlayRequest{Mutation: mutation, AgentRef: agentRef, PublishedOverlayRef: body.PublishedOverlayRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

// BindAgentRuntimeEnvironment sends the selected environment and mutation pins to the dedicated owner binding command, returning an exact-agent validated configuration without inferring authority from the environment reference.
// BindAgentRuntimeEnvironment передаёт выбранное окружение и mutation pins отдельной команде привязки владельца и выдаёт проверенную конфигурацию точного сотрудника без вывода полномочий из ссылки окружения.
func (server *Server) BindAgentRuntimeEnvironment(writer http.ResponseWriter, request *http.Request, agentRef generated.AgentRef, parameters generated.BindAgentRuntimeEnvironmentParams) {
	body, ok := decodeJSON[generated.RuntimeEnvironmentBindingInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.BindAgentRuntimeEnvironment(request.Context(), &controlplanev1.BindAgentRuntimeEnvironmentRequest{Mutation: mutation, AgentRef: agentRef, EnvironmentRef: body.EnvironmentRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeAgentRuntimeConfiguration(writer, http.StatusOK, response.GetRuntimeConfiguration(), agentRef)
}

// writeAgentRuntimeConfiguration requires a view with a valid agent version, exact configuration agent, at most128 combined context bindings and unique valid binding/resource references before common projection. Validation of returned metadata does not itself issue binding grants.
// writeAgentRuntimeConfiguration требует представление с допустимой версией, точным сотрудником конфигурации, не более128 общих context bindings и уникальными допустимыми ссылками bindings и ресурсов до общей проекции. Проверка metadata сама grants не выдаёт.
func writeAgentRuntimeConfiguration(writer http.ResponseWriter, statusCode int, view *controlplanev1.AgentRuntimeConfigurationView, agentRef string) {
	if view == nil || !validManagedVersion(view.GetAgentVersion()) || view.GetConfiguration().GetAgentRef() != agentRef || len(view.GetSkillBindings())+len(view.GetMemoryBindings()) > 128 {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	seenRefs, seenResources := make(map[string]bool), make(map[string]bool)
	for _, bindings := range [][]*controlplanev1.AgentContextBinding{view.GetSkillBindings(), view.GetMemoryBindings()} {
		for _, binding := range bindings {
			if !validAgentContextBinding(binding, agentRef) || seenRefs[binding.GetRef()] || seenResources[binding.GetResourceRef()] {
				writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
				return
			}
			seenRefs[binding.GetRef()], seenResources[binding.GetResourceRef()] = true, true
		}
	}
	writeMessage(writer, statusCode, view, "", "")
}

// ListRuntimeEnvironmentSets derives route-project context and forwards query/page to the owner catalog without publishing or selecting an environment.
// ListRuntimeEnvironmentSets выводит контекст проекта маршрута и передаёт поиск и страницу каталогу владельца без публикации и выбора окружения.
func (server *Server) ListRuntimeEnvironmentSets(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.ListRuntimeEnvironmentSetsParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	response, err := server.control.Query.ListRuntimeEnvironmentSets(request.Context(), &controlplanev1.ListRuntimeEnvironmentSetsRequest{
		ProjectRef: projectRef, Query: stringValue(parameters.Query), Page: page(parameters.PageSize, parameters.PageToken),
	})
	writeReadRPCResult(writer, response, err, "", "environments")
}

// GetRuntimeEnvironmentSet resolves one selected environment through owner policy and emits public metadata, not mounted values or secret material.
// GetRuntimeEnvironmentSet разрешает выбранное окружение по политике владельца и выдаёт публичную metadata, а не смонтированные значения и секреты.
func (server *Server) GetRuntimeEnvironmentSet(writer http.ResponseWriter, request *http.Request, environmentRef generated.RuntimeEnvironmentRef) {
	response, err := server.control.Query.GetRuntimeEnvironmentSet(request.Context(), &controlplanev1.GetRuntimeEnvironmentSetRequest{EnvironmentRef: environmentRef})
	writeReadRPCResult(writer, response, err, "environment", "")
}

// ListRuntimeEnvironmentVersions reads exact-environment historical metadata with owner pagination without promoting or rolling back a version.
// ListRuntimeEnvironmentVersions читает историческую metadata точного окружения с пагинацией владельца без продвижения и отката версии.
func (server *Server) ListRuntimeEnvironmentVersions(writer http.ResponseWriter, request *http.Request, environmentRef generated.RuntimeEnvironmentRef, parameters generated.ListRuntimeEnvironmentVersionsParams) {
	response, err := server.control.Query.ListRuntimeEnvironmentVersions(request.Context(), &controlplanev1.ListRuntimeEnvironmentVersionsRequest{
		EnvironmentRef: environmentRef, Page: page(parameters.PageSize, parameters.PageToken),
	})
	writeReadRPCResult(writer, response, err, "", "versions")
}

// CreateRuntimeEnvironmentSet binds route-project context, decodes the body, validates secret revision pins and policy, then sends a specialized idempotent owner create and projects HTTP201. Secret values are neither revealed nor substituted here.
// CreateRuntimeEnvironmentSet привязывает проект маршрута, разбирает тело, проверяет pins ревизий секретов и политику, затем отправляет отдельный идемпотентный create владельцу и проецирует HTTP201. Значения секретов здесь не раскрываются и не подставляются.
func (server *Server) CreateRuntimeEnvironmentSet(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.CreateRuntimeEnvironmentSetParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.RuntimeEnvironmentInput](writer, request)
	if !ok {
		return
	}
	bindings, ok := runtimeSecretBindings(writer, body.SecretBindings)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	policy, ok := runtimeEnvironmentPolicyInput(body.Policy)
	if !ok {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Command.CreateRuntimeEnvironmentSet(request.Context(), &controlplanev1.CreateRuntimeEnvironmentSetRequest{
		Mutation: mutation, ProjectRef: projectRef, Name: body.Name, Description: body.Description,
		ImageArtifactRef: body.ImageArtifactRef, Values: runtimeEnvironmentValues(body.Values),
		SecretBindings: bindings, Tools: runtimeEnvironmentTools(body.Tools),
		Policy: policy,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "environment", "")
}

// PublishRuntimeEnvironmentVersion validates body, secret revision bounds, mutation pins and typed policy before the dedicated owner publication. Resource admission, image validation and materialization remain owner responsibilities rather than effects of this mapper.
// PublishRuntimeEnvironmentVersion проверяет тело, пределы ревизий секретов, mutation pins и типизированную политику до отдельной публикации владельца. Допуск ресурсов, проверка образа и материализация остаются ответственностью владельца, а не эффектами mapper.
func (server *Server) PublishRuntimeEnvironmentVersion(writer http.ResponseWriter, request *http.Request, environmentRef generated.RuntimeEnvironmentRef, parameters generated.PublishRuntimeEnvironmentVersionParams) {
	body, ok := decodeJSON[generated.RuntimeEnvironmentInput](writer, request)
	if !ok {
		return
	}
	bindings, ok := runtimeSecretBindings(writer, body.SecretBindings)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	policy, ok := runtimeEnvironmentPolicyInput(body.Policy)
	if !ok {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Command.PublishRuntimeEnvironmentVersion(request.Context(), &controlplanev1.PublishRuntimeEnvironmentVersionRequest{
		Mutation: mutation, EnvironmentRef: environmentRef, Name: body.Name, Description: body.Description,
		ImageArtifactRef: body.ImageArtifactRef, Values: runtimeEnvironmentValues(body.Values),
		SecretBindings: bindings, Tools: runtimeEnvironmentTools(body.Tools),
		Policy: policy,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "environment", "")
}

// RollbackRuntimeEnvironment forwards the selected published-version reference and mutation context to the owner rollback and projects the environment without rewriting version history locally.
// RollbackRuntimeEnvironment передаёт выбранную опубликованную версию и mutation context rollback владельца и проецирует окружение без локального переписывания истории.
func (server *Server) RollbackRuntimeEnvironment(writer http.ResponseWriter, request *http.Request, environmentRef generated.RuntimeEnvironmentRef, parameters generated.RollbackRuntimeEnvironmentParams) {
	body, ok := decodeJSON[generated.RuntimeEnvironmentRollbackInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RollbackRuntimeEnvironment(request.Context(), &controlplanev1.RollbackRuntimeEnvironmentRequest{Mutation: mutation, EnvironmentRef: environmentRef, PublishedVersionRef: body.PublishedVersionRef})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, response, "environment", "")
}

// ListTemplateVariables adapts route-project and optional agent/runtime/query/source/page selectors to the shared template-variable endpoint implementation without evaluating a template.
// ListTemplateVariables передаёт проект маршрута и необязательные селекторы сотрудника, runtime, поиска, источника и страницы общей реализации каталога переменных без вычисления шаблона.
func (server *Server) ListTemplateVariables(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.ListTemplateVariablesParams) {
	server.listTemplateVariables(writer, request, projectRef, stringValue(parameters.AgentRef), stringValue(parameters.RuntimeRevisionRef), stringValue(parameters.Query), stringValue(parameters.Source), parameters.PageSize, parameters.PageToken)
}

// providerAccountCandidates requires one to thirty-two distinct pacc references, weights1..100, a valid provider key and exact mcat_/digest pairing, returning typed candidates in input order. It does not select an account, fetch a catalog or assign default reasoning effort.
// providerAccountCandidates требует от одного до тридцати двух различных pacc-ссылок, веса1..100, допустимый provider key и точную пару mcat_/digest, сохраняя порядок. Аккаунт не выбирает, каталог не читает и default reasoning effort не назначает.
func providerAccountCandidates(input []generated.ProviderAccountCandidateInput) ([]*controlplanev1.ProviderAccountCandidate, bool) {
	result := make([]*controlplanev1.ProviderAccountCandidate, 0, len(input))
	seen := map[string]bool{}
	for _, item := range input {
		if seen[item.AccountRef] || !opaqueHTTPReference.MatchString(item.AccountRef) || !strings.HasPrefix(item.AccountRef, "pacc_") || len(item.AccountRef) > 96 || item.Weight < 1 || item.Weight > 100 || !modelProviderKey.MatchString(item.ProviderDefinitionKey) || !validManagedDigest(item.CatalogDigest) || item.CatalogRevision != "mcat_"+item.CatalogDigest {
			return nil, false
		}
		seen[item.AccountRef] = true
		result = append(result, &controlplanev1.ProviderAccountCandidate{AccountRef: item.AccountRef, Weight: int32(item.Weight), CatalogRevision: item.CatalogRevision, CatalogDigest: item.CatalogDigest, ProviderDefinitionKey: item.ProviderDefinitionKey})
	}
	return result, len(input) > 0 && len(input) <= 32
}

// runtimeEnvironmentValues copies names and nonsecret values into a fresh typed slice, preserving order/empty strings without validating names, uniqueness or expanding templates.
// runtimeEnvironmentValues копирует имена и несекретные значения в новый типизированный slice с сохранением порядка и пустых строк без проверки имён, уникальности и раскрытия шаблонов.
func runtimeEnvironmentValues(input []generated.RuntimeEnvironmentValue) []*controlplanev1.RuntimeEnvironmentValue {
	result := make([]*controlplanev1.RuntimeEnvironmentValue, 0, len(input))
	for _, item := range input {
		result = append(result, &controlplanev1.RuntimeEnvironmentValue{Name: item.Name, Value: item.Value})
	}
	return result
}

// runtimeSecretBindings copies binding metadata and optional revision pins, representing absence as zero and rejecting negative or browser-unsafe revisions with INVALID_REQUEST. It does not read secrets or validate resource ownership.
// runtimeSecretBindings копирует metadata привязок и необязательные ревизии, обозначая отсутствие нулём и отклоняя отрицательные и небезопасные для браузера ревизии с INVALID_REQUEST. Секреты и владение ресурсами не проверяются.
func runtimeSecretBindings(w http.ResponseWriter, input []generated.RuntimeSecretBinding) ([]*controlplanev1.RuntimeSecretBinding, bool) {
	result := make([]*controlplanev1.RuntimeSecretBinding, 0, len(input))
	for _, item := range input {
		revision := int64(0)
		if item.Revision != nil {
			revision = *item.Revision
		}
		if revision < 0 || revision > maximumSafeJSONInteger {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return nil, false
		}
		result = append(result, &controlplanev1.RuntimeSecretBinding{Name: item.Name, SecretRef: item.SecretRef, Revision: revision})
	}
	return result, true
}

// runtimeEnvironmentTools copies tool name, command, description and usage into a fresh typed slice without executing commands or admitting tools.
// runtimeEnvironmentTools копирует имя, команду, описание и способ применения инструментов в новый slice без выполнения команд и допуска инструментов.
func runtimeEnvironmentTools(input []generated.RuntimeEnvironmentTool) []*controlplanev1.RuntimeEnvironmentTool {
	result := make([]*controlplanev1.RuntimeEnvironmentTool, 0, len(input))
	for _, item := range input {
		result = append(result, &controlplanev1.RuntimeEnvironmentTool{Name: item.Name, Command: item.Command, Description: item.Description, UsageHint: item.UsageHint})
	}
	return result
}

// runtimeEnvironmentPolicyInput maps resources, web rules and closed Kubernetes/volume/destination enums, enforcing optional workspace bounds1..1GiB and1..10000files. It returns no result on its checked failures; copied CPU/memory, web rules and other fields still require authoritative owner validation and network enforcement.
// runtimeEnvironmentPolicyInput преобразует ресурсы, web rules и закрытые Kubernetes/volume/destination enum, проверяя необязательные пределы workspace1..1ГиБ и1..10000файлов. При своих отказах результат не выдаёт; скопированные CPU/memory, web rules и другие поля ещё требуют проверки владельца и сетевого исполнения.
func runtimeEnvironmentPolicyInput(input generated.RuntimeEnvironmentPolicyInput) (*controlplanev1.RuntimeEnvironmentPolicyInput, bool) {
	if !input.KubernetesAccess.Valid() {
		return nil, false
	}
	result := &controlplanev1.RuntimeEnvironmentPolicyInput{Resources: &controlplanev1.RuntimeResourcePolicy{
		CpuRequestMilli: input.Resources.CpuRequestMilli, CpuLimitMilli: input.Resources.CpuLimitMilli,
		MemoryRequestMib: input.Resources.MemoryRequestMib, MemoryLimitMib: input.Resources.MemoryLimitMib,
		EphemeralStorageRequestMib: input.Resources.EphemeralStorageRequestMib,
		EphemeralStorageLimitMib:   input.Resources.EphemeralStorageLimitMib,
	}, KubernetesAccess: runtimeKubernetesAccessKind(string(input.KubernetesAccess)),
		WebAccess: &controlplanev1.RuntimeWebAccess{Mode: runtimeWebAccessMode(string(input.WebAccess.Mode))}}
	if limits := input.Resources.WorkspaceLimits; limits != nil {
		if limits.MaxBytes < 1 || limits.MaxBytes > runtimecontract.RuntimeWorkspaceWritableBytes || limits.MaxFiles < 1 || limits.MaxFiles > runtimecontract.RuntimeWorkspaceMaximumFiles {
			return nil, false
		}
		result.Resources.WorkspaceLimits = &controlplanev1.RuntimeWorkspaceLimits{MaxBytes: limits.MaxBytes, MaxFiles: limits.MaxFiles}
	}
	for _, rule := range input.WebAccess.Rules {
		methods := make([]string, 0, len(rule.HttpMethods))
		for _, method := range rule.HttpMethods {
			methods = append(methods, string(method))
		}
		result.WebAccess.Rules = append(result.WebAccess.Rules, &controlplanev1.RuntimeWebAccessRule{
			DomainPattern: rule.DomainPattern, Protocol: string(rule.Protocol), Port: int32(rule.Port), HttpMethods: methods,
		})
	}
	for _, volume := range input.Volumes {
		if !volume.Kind.Valid() {
			return nil, false
		}
		result.Volumes = append(result.Volumes, &controlplanev1.RuntimeVolumeInput{
			Name: volume.Name, Kind: runtimeVolumeKind(string(volume.Kind)), SizeMib: volume.SizeMib,
		})
	}
	for _, destination := range input.NetworkDestinations {
		if !destination.Valid() {
			return nil, false
		}
		result.NetworkDestinations = append(result.NetworkDestinations, runtimeNetworkDestination(string(destination)))
	}
	return result, true
}

// runtimeWebAccessMode maps four known public web modes and returns unspecified for an unknown string, without validating rules or granting egress.
// runtimeWebAccessMode преобразует четыре известных публичных web-режима, возвращая unspecified неизвестной строке без проверки правил и выдачи egress.
func runtimeWebAccessMode(value string) controlplanev1.RuntimeWebAccessMode {
	switch value {
	case "NONE":
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_NONE
	case "ALLOWLIST_READ_ONLY":
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_ALLOWLIST_READ_ONLY
	case "ALLOWLIST_FULL":
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_ALLOWLIST_FULL
	case "FULL_PUBLIC":
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_FULL_PUBLIC
	default:
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_UNSPECIFIED
	}
}

// runtimeVolumeKind maps disk/memory ephemeral kinds to typed enums and unknown input to unspecified without provisioning storage.
// runtimeVolumeKind преобразует временные дисковый и memory виды в enum, неизвестный ввод — в unspecified, не создавая хранилище.
func runtimeVolumeKind(value string) controlplanev1.RuntimeVolumeKind {
	switch value {
	case "EPHEMERAL_DISK":
		return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK
	case "EPHEMERAL_MEMORY":
		return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY
	default:
		return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_UNSPECIFIED
	}
}

// runtimeNetworkDestination maps only DNS, callback and provider-proxy destination names; unspecified fallback is not a wildcard network grant.
// runtimeNetworkDestination преобразует только имена DNS, callback и provider-proxy; unspecified не является wildcard сетевым grant.
func runtimeNetworkDestination(value string) controlplanev1.RuntimeNetworkDestination {
	switch value {
	case "DNS":
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS
	case "RUNTIME_CALLBACK":
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK
	case "PROVIDER_PROXY":
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY
	default:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_UNSPECIFIED
	}
}

// runtimeKubernetesAccessKind recognizes only NONE and returns unspecified for any other input; it does not issue Kubernetes credentials or permissions.
// runtimeKubernetesAccessKind распознаёт только NONE, возвращая unspecified для другого ввода; Kubernetes credentials и права не выдаёт.
func runtimeKubernetesAccessKind(value string) controlplanev1.RuntimeKubernetesAccessKind {
	switch value {
	case "NONE":
		return controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE
	default:
		return controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_UNSPECIFIED
	}
}
